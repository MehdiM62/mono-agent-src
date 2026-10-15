package apipkg

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// shapeOutput applies output.each, output.expand and output.match to a
// result list.
func (r *opRun) shapeOutput(ctx context.Context, v any) (any, error) {
	o := r.op.Output
	if o == nil {
		return v, nil
	}
	list, isList := v.([]any)
	if !isList {
		// A single object is shaped as a one-element list (Coda getRow).
		if _, isObj := v.(map[string]any); !isObj {
			return v, nil
		}
		shaped, err := r.shapeOutput(ctx, []any{v})
		if err != nil {
			return nil, err
		}
		if l := shaped.([]any); len(l) == 1 {
			return l[0], nil
		} else if len(l) == 0 {
			return nil, nil
		}
		return shaped, nil
	}
	each, err := r.renderOutputKey(o.Each)
	if err != nil {
		return nil, r.errf("output.each: %w", err)
	}
	if each != "" {
		path := strings.Split(each, ".")
		out := make([]any, 0, len(list))
		for _, el := range list {
			if x := lookupPath(el, path); x != nil {
				out = append(out, x)
			}
		}
		list = out
	}
	if st := o.Expand; st != nil {
		for i, el := range list {
			sc := r.scope
			sc.El = el
			resp, err := r.sendStep(ctx, *st, sc)
			if err != nil {
				return nil, fmt.Errorf("output.expand element %d: %w", i, err)
			}
			if resp == nil || len(strings.TrimSpace(string(resp.body))) == 0 {
				continue
			}
			got := decodeBody(resp.body)
			if o.ExpandMergeKey == "" {
				list[i] = got
				continue
			}
			if o.ExpandMergePath != "" {
				got = lookupPath(got, strings.Split(o.ExpandMergePath, "."))
			}
			if m, ok := el.(map[string]any); ok {
				m[o.ExpandMergeKey] = got
			}
		}
	}
	if o.Download != nil {
		if err := r.downloadFiles(ctx, list); err != nil {
			return nil, err
		}
	}
	if o.Where != "" {
		if list, err = r.applyWhere(list); err != nil {
			return nil, err
		}
	}
	if !o.MatchAfterTo {
		if list, err = r.applyMatch(list); err != nil {
			return nil, err
		}
	}
	if o.Firestore != "" {
		on, err := renderString(o.Firestore, r.scope)
		if err != nil {
			return nil, r.errf("output.firestore: %w", err)
		}
		if b, _ := toBool(on); b == true {
			for i, el := range list {
				list[i] = firestoreDoc(el)
			}
		}
	}
	if o.Skip > 0 {
		if o.Skip >= len(list) {
			list = nil
		} else {
			list = list[o.Skip:]
		}
	}
	if o.Sort != "" {
		path := strings.Split(o.Sort, ".")
		sort.SliceStable(list, func(i, j int) bool {
			a, b := lookupPath(list[i], path), lookupPath(list[j], path)
			less := lessValue(a, b)
			if o.SortDesc {
				return lessValue(b, a)
			}
			return less
		})
	}
	if o.Limit {
		list = r.clientLimit(list)
	}
	if o.Merge {
		merged := map[string]any{}
		for _, el := range list {
			if m, ok := el.(map[string]any); ok {
				for k, x := range m {
					merged[k] = x
				}
			}
		}
		list = []any{merged}
	}
	if o.To != nil {
		apply := true
		if o.ToWhen != "" {
			w, err := renderString(o.ToWhen, r.scope)
			if err != nil {
				return nil, r.errf("output.to_when: %w", err)
			}
			apply = truthy(w)
		}
		if apply {
			for i, el := range list {
				sc := r.scope
				sc.El = el
				x, err := renderValue(o.To, sc)
				if err != nil {
					return nil, r.errf("output.to element %d: %w", i, err)
				}
				carryFiles(el, x)
				list[i] = x
			}
		}
	}
	if o.MatchAfterTo {
		if list, err = r.applyMatch(list); err != nil {
			return nil, err
		}
	}
	return list, nil
}

// renderOutputKey renders an output path/each value, which may be a
// template over p (n8n rawData/simple toggles); empty means none.
func (r *opRun) renderOutputKey(s string) (string, error) {
	if !hasTemplate(s) {
		return s, nil
	}
	return renderText(s, r.scope)
}

// outputPath is the rendered output.path ("" when unset or empty).
func (r *opRun) outputPath() string {
	if r.op.Output == nil {
		return ""
	}
	p, err := r.renderOutputKey(r.op.Output.Path)
	if err != nil {
		return r.op.Output.Path
	}
	return p
}

// downloads reports whether this run's response is a file.
func (r *opRun) downloads() bool {
	b := r.op.Binary
	if b == nil {
		return false
	}
	if b.DownloadWhen == "" {
		return b.Download
	}
	w, err := renderString(b.DownloadWhen, r.scope)
	return err == nil && truthy(w)
}

// poll repeats op.Poll after the main response until its until template
// (res = the latest response) is true, waiting interval_ms between tries
// and failing after max_wait_ms.
func (r *opRun) poll(ctx context.Context, resp *response) (*response, error) {
	pl := r.op.Poll
	if pl == nil {
		return resp, nil
	}
	interval := time.Duration(pl.IntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = time.Second
	}
	maxWait, err := r.waitFor(pl.MaxWaitMs, pl.MaxWaitSeconds, r.scope, "poll.max_wait_seconds")
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(maxWait)
	first := decodeBody(resp.body)
	r.scope.Main = first // output templates read the first reply as main
	for {
		sc := r.scope
		sc.Main = first
		sc.Res = decodeBody(resp.body)
		done, err := renderString(pl.Until, sc)
		if err != nil {
			return nil, r.errf("poll.until: %w", err)
		}
		if b, _ := toBool(done); b == true {
			return resp, nil
		}
		if time.Now().After(deadline) {
			return nil, r.errf("poll: not done after %s", maxWait)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		next, err := r.sendStep(ctx, pl.Step, sc)
		if err != nil {
			return nil, fmt.Errorf("poll: %w", err)
		}
		if next != nil {
			resp = next
		}
	}
}

// schemaRows turns BigQuery rows ({f: [{v}]}) into objects keyed by the
// schema's field names; RECORD fields recurse, REPEATED ones are lists.
func schemaRows(v any, fields []any, nums bool) any {
	rows, ok := v.([]any)
	if !ok || len(fields) == 0 {
		return v
	}
	out := make([]any, len(rows))
	for i, row := range rows {
		out[i] = schemaRow(row, fields, nums)
	}
	return out
}

func schemaRow(row any, fields []any, nums bool) any {
	cells, _ := lookupPath(row, []string{"f"}).([]any)
	obj := map[string]any{}
	for i, f := range fields {
		fm, _ := f.(map[string]any)
		name := stringify(fm["name"])
		if name == "" || i >= len(cells) {
			continue
		}
		obj[name] = schemaCell(lookupPath(cells[i], []string{"v"}), fm, nums)
	}
	return obj
}

func schemaCell(v any, f map[string]any, nums bool) any {
	if v == nil {
		return nil
	}
	if stringify(f["mode"]) == "REPEATED" {
		list, _ := v.([]any)
		out := make([]any, len(list))
		single := map[string]any{}
		for k, x := range f {
			if k != "mode" {
				single[k] = x
			}
		}
		for i, el := range list {
			out[i] = schemaCell(lookupPath(el, []string{"v"}), single, nums)
		}
		return out
	}
	switch stringify(f["type"]) {
	case "RECORD", "STRUCT":
		sub, _ := f["fields"].([]any)
		return schemaRow(v, sub, nums)
	case "INTEGER", "INT64", "NUMERIC", "BIGNUMERIC", "FLOAT", "FLOAT64":
		if nums {
			if n, err := strconv.ParseFloat(stringify(v), 64); err == nil {
				return n
			}
		}
	case "BOOLEAN", "BOOL":
		if b, err := strconv.ParseBool(stringify(v)); err == nil {
			return b
		}
	}
	return v
}

// clientLimit honours the op's returnAll/limit params on a list the API
// returned whole (n8n slices client-side).
func (r *opRun) clientLimit(list []any) []any {
	if b, _ := toBool(r.params["returnAll"]); b == true {
		return list
	}
	n, _ := toInt(r.params["limit"])
	if l, ok := n.(int64); ok && l > 0 && int(l) < len(list) {
		return list[:l]
	}
	return list
}

// lessValue orders numbers numerically and anything else as text.
func lessValue(a, b any) bool {
	fa, ea := toFloat(a)
	fb, eb := toFloat(b)
	if ea == nil && eb == nil && fa != nil && fb != nil {
		return fa.(float64) < fb.(float64)
	}
	return stringify(a) < stringify(b)
}

// pagingOn reports whether pagination applies to this run.
func (r *opRun) pagingOn() bool {
	w := r.op.Pagination.When
	if w == "" {
		return true
	}
	v, err := renderString(w, r.scope)
	return err == nil && truthy(v)
}

// client is the HTTP client for this run: a connection whose
// allow_unauthorized_certs field is true (self-hosted Venafi, TheHive with
// self-signed certificates) skips TLS verification, as n8n's credential
// option does.
func (r *opRun) client() *http.Client {
	if !truthy(r.scope.Cred["allow_unauthorized_certs"]) {
		return httpClient
	}
	insecureOnce.Do(func() {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // user opted in per connection
		insecureClient = &http.Client{Timeout: httpClient.Timeout, Transport: t}
	})
	if httpClient.Transport != nil && httpClient.Transport != http.DefaultTransport {
		return httpClient // tests reroute traffic through their own transport
	}
	return insecureClient
}

var (
	insecureOnce   sync.Once
	insecureClient *http.Client
)
