package apipkg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// execute sends the request (all pages) and turns the response into items.
func (r *opRun) execute(ctx context.Context) ([]workflow.Item, error) {
	for i, st := range r.op.Before {
		if st.ForEach != "" {
			lv, err := renderString(st.ForEach, r.scope)
			if err != nil {
				return nil, r.errf("before[%d].for_each: %w", i, err)
			}
			list, _ := lv.([]any)
			collected := make([]any, 0, len(list))
			for j, el := range list {
				sc := r.scope
				sc.El, sc.Idx = el, j
				step := *r
				step.scope = sc
				if s, ok := el.(string); ok && st.Upload {
					step.prop = s // one upload per binary property (Twist)
				}
				resp, err := step.sendStep(ctx, st, sc)
				if err != nil {
					return nil, fmt.Errorf("before[%d] element %d: %w", i, j, err)
				}
				var v any
				if resp != nil && len(strings.TrimSpace(string(resp.body))) > 0 {
					v = decodeBody(resp.body)
				}
				collected = append(collected, v)
			}
			r.scope.Pres = append(r.scope.Pres, collected)
			r.scope.Pre = collected
			continue
		}
		resp, err := r.sendStep(ctx, st, r.scope)
		if err != nil {
			return nil, fmt.Errorf("before[%d]: %w", i, err)
		}
		var v any
		if resp != nil && len(strings.TrimSpace(string(resp.body))) > 0 {
			v = decodeBody(resp.body)
		}
		r.scope.Pres = append(r.scope.Pres, v)
		r.scope.Pre = v
		if resp != nil {
			// The main request may read a before step's headers (a
			// resumable upload's X-Goog-Upload-Url).
			r.scope.Hdr = headerMap{}
			for k, vs := range resp.header {
				if len(vs) > 0 {
					r.scope.Hdr[k] = vs[0]
				}
			}
		}
	}
	if r.op.Pagination != nil && r.pagingOn() {
		results, truncated, err := r.paginate(ctx)
		if err != nil {
			return nil, err
		}
		// also steps follow the last page (Elasticsearch closes its PIT
		// with the id the last page returned); main is the first page.
		if err := r.runAlso(ctx, r.lastPage, r.scope.Res); err != nil {
			return nil, err
		}
		var all any = results
		if o := r.op.Output; o != nil && o.Walk != nil {
			if all, err = r.walk(ctx, results, results); err != nil {
				return nil, err
			}
		}
		shaped, err := r.shapeOutput(ctx, all)
		if err != nil {
			return nil, err
		}
		items := toItems(shaped, true)
		if truncated {
			for i := range items {
				items[i].JSON["pagination_truncated"] = true
			}
		}
		return items, nil
	}
	resp, err := r.sendMain(ctx)
	if err != nil {
		return nil, err
	}
	if resp, err = r.poll(ctx, resp); err != nil {
		return nil, err
	}
	var res any
	if len(strings.TrimSpace(string(resp.body))) > 0 {
		res = decodeBody(resp.body)
	}
	if err := r.runAlso(ctx, res, res); err != nil {
		return nil, err
	}
	r.scope.Hdr = headerMap{}
	for k, vs := range resp.header {
		if len(vs) > 0 {
			r.scope.Hdr[k] = vs[0]
		}
	}
	if r.downloads() {
		if b := r.op.Binary; b.DownloadBase64 != "" {
			return r.downloadBase64Item(resp)
		}
		return []workflow.Item{r.downloadItem(resp)}, nil
	}
	if len(strings.TrimSpace(string(resp.body))) == 0 {
		// An empty body (201 + Location/Resource-ID headers) can still be
		// shaped from hdr by output.to / output.expand.
		if o := r.op.Output; o != nil && (o.To != nil || o.Expand != nil) {
			shaped, err := r.shapeOutput(ctx, map[string]any{})
			if err != nil {
				return nil, err
			}
			return toItems(shaped, true), nil
		}
		return []workflow.Item{{JSON: map[string]any{"success": true}}}, nil
	}
	v := decodeBody(resp.body)
	if o := r.op.Output; o != nil && !isEmpty(o.From) {
		sc := r.scope
		sc.Res = v
		fv, err := renderValue(o.From, sc)
		if err != nil {
			return nil, r.errf("output.from: %w", err)
		}
		v = fv
	}
	doc := v
	if path := r.outputPath(); path != "" {
		v = lookupPath(v, strings.Split(path, "."))
	}
	if o := r.op.Output; o != nil && o.SchemaRows != "" {
		fields, _ := lookupPath(doc, strings.Split(o.SchemaRows, ".")).([]any)
		nums := false
		if o.Numbers != "" {
			on, err := renderString(o.Numbers, r.scope)
			if err != nil {
				return nil, r.errf("output.numbers: %w", err)
			}
			b, _ := toBool(on)
			nums = b == true
		}
		v = schemaRows(v, fields, nums)
	}
	if r.op.Output != nil && r.op.Output.HeaderRow {
		v = rowsToObjects(v)
	}
	if o := r.op.Output; o != nil && o.Walk != nil {
		if v, err = r.walk(ctx, doc, v); err != nil {
			return nil, err
		}
	}
	r.scope.Res = doc // output.to/match templates may read the whole response
	if v, err = r.shapeOutput(ctx, v); err != nil {
		return nil, err
	}
	split := r.op.Output == nil || r.op.Output.Split == nil || *r.op.Output.Split
	return toItems(v, split), nil
}

// response is a 2xx response.
type response struct {
	status int
	header http.Header
	body   []byte
	url    string
}

// send builds and sends one request; non-2xx responses are errors.
func (r *opRun) send(ctx context.Context, pp pageParams) (*response, error) {
	if r.op.Stream != nil {
		return r.sendStream(ctx, pp)
	}
	b, err := r.buildRequest(ctx, pp)
	if err != nil {
		return nil, err
	}
	resp, err := r.client().Do(b.req)
	if err != nil {
		return nil, r.errf("%s %s: %w", b.req.Method, b.req.URL.Redacted(), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return nil, r.errf("reading response: %w", err)
	}
	if len(body) > maxResponseBody {
		return nil, r.errf("response body exceeds %d MiB", maxResponseBody>>20)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, r.errf("%w", &statusError{code: resp.StatusCode, body: errorBodyEcho(body)})
	}
	if err := r.checkErrorWhen(body, resp.Header); err != nil {
		return nil, err
	}
	return &response{status: resp.StatusCode, header: resp.Header, body: body, url: b.req.URL.String()}, nil
}

// statusError is a non-2xx response; callers that treat a status as an
// answer (a webhook delete's 404) find it with errors.As.
type statusError struct {
	code int
	body string
}

func (e *statusError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.code, e.body) }

func errorBodyEcho(b []byte) string {
	if len(b) > maxErrorBodyEcho {
		return string(b[:maxErrorBodyEcho]) + fmt.Sprintf("…(%d bytes truncated)", len(b)-maxErrorBodyEcho)
	}
	return string(b)
}

// downloadItem stores a file response on the item's binary data.
func (r *opRun) downloadItem(resp *response) workflow.Item {
	prop := r.downloadProperty()
	ct := resp.header.Get("Content-Type")
	name := ""
	if _, params, err := mime.ParseMediaType(resp.header.Get("Content-Disposition")); err == nil {
		name = params["filename"]
	}
	// An op that uploads nothing may name the file it downloads
	// (binary.file_name / mime_type) for responses that don't.
	if b := r.op.Binary; b.UploadField == "" && isEmpty(b.Properties) {
		if name == "" {
			name, _ = renderText(b.FileName, r.scope)
		}
		if ct == "" {
			ct, _ = renderText(b.MimeType, r.scope)
		}
	}
	return workflow.Item{
		JSON: map[string]any{
			"mime_type": ct, "file_name": name, "size": len(resp.body), "binary_property": prop,
		},
		Binary: map[string][]byte{prop: resp.body},
	}
}

// decodeBody parses JSON, falling back to the text.
func decodeBody(b []byte) any {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		if t := bytes.TrimSpace(b); len(t) > 0 && t[0] == '<' {
			if x, err := decodeXML(t); err == nil {
				return x // WebDAV multistatus, SOAP-ish APIs
			}
		}
		return string(b)
	}
	return v
}

// toItems turns a value into items: an array splits into one item per
// element (when split), an object is one item, anything else is
// {"data": value}.
func toItems(v any, split bool) []workflow.Item {
	if arr, ok := v.([]any); ok && split {
		items := make([]workflow.Item, 0, len(arr))
		for _, el := range arr {
			it := workflow.Item{JSON: asObject(el)}
			takeFiles(&it)
			items = append(items, it)
		}
		return items
	}
	if v == nil {
		return []workflow.Item{}
	}
	it := workflow.Item{JSON: asObject(v)}
	takeFiles(&it)
	return []workflow.Item{it}
}

func asObject(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{"data": v}
}

// runAlso sends the operation's follow-up requests in order, each with res
// bound to the main response (the last page of a paginated op) and main to
// the first. A step whose "when" renders empty is skipped.
func (r *opRun) runAlso(ctx context.Context, res, main any) error {
	if len(r.op.Also) == 0 {
		return nil
	}
	for i, st := range r.op.Also {
		sc := r.scope
		sc.Res, sc.Main = res, main
		if st.ForEach != "" {
			lv, err := renderString(st.ForEach, sc)
			if err != nil {
				return r.errf("also[%d].for_each: %w", i, err)
			}
			list, _ := lv.([]any)
			for j, el := range list {
				esc := sc
				esc.El = el
				if _, err := r.sendStep(ctx, st, esc); err != nil {
					return fmt.Errorf("also[%d] element %d: %w", i, j, err)
				}
			}
			continue
		}
		if _, err := r.sendStep(ctx, st, sc); err != nil {
			return fmt.Errorf("also[%d]: %w", i, err)
		}
	}
	return nil
}

// ScopeCheck, when set, checks that a credential grants an umbrella scope
// group (see Credential.ScopeGroup). It is wired to
// connections.CheckCredentialScopes by the registry.
var ScopeCheck func(cred map[string]any, group string) error

// sendStep sends one extra request (before/also) of the operation with
// scope sc and the operation's auth.
func (r *opRun) sendStep(ctx context.Context, st Step, sc Scope) (*response, error) {
	if st.When != "" {
		w, err := renderString(st.When, sc)
		if err != nil {
			return nil, r.errf("when: %w", err)
		}
		if !truthy(w) {
			return nil, nil
		}
	}
	op := *r.op
	op.Method, op.Path, op.BaseURL = st.Method, st.Path, st.BaseURL
	op.Query, op.Body, op.BodyType, op.Headers = st.Query, st.Body, st.BodyType, st.Headers
	op.Pagination, op.Also, op.Before, op.Stream = nil, nil, nil, st.Stream
	if st.ErrorWhen != "" {
		op.ErrorWhen, op.ErrorMessage = st.ErrorWhen, st.ErrorMessage
	}
	if st.Upload && r.op.Binary != nil && r.op.Binary.UploadField != "" {
		b := *r.op.Binary
		b.UploadWhen, b.DownloadWhen, b.Download, b.DownloadBase64 = "", "", false, ""
		op.Binary = &b
		if st.BodyType == BodyMultipart {
			// S3-style presigned POST: the step's form fields plus the file.
			op.BodyType = BodyMultipart
		} else {
			op.Body, op.BodyType = nil, BodyRaw
		}
	} else if st.download {
		op.Binary = &Binary{Download: true}
	} else {
		op.Binary = nil
	}
	step := *r
	step.op, step.scope = &op, sc
	if st.Auth != nil && !*st.Auth {
		step.credDef = nil
	}
	resp, err := step.send(ctx, pageParams{})
	if err != nil || st.Repeat == nil {
		return resp, err
	}
	return r.repeatStep(ctx, st, sc, resp)
}
