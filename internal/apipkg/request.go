package apipkg

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Body types.
const (
	BodyJSON      = "json"
	BodyForm      = "form"
	BodyMultipart = "multipart"
	BodyRaw       = "raw"
	BodyNone      = "none"
	// BodyNDJSON: the body renders to a list, one JSON line per element
	// (an element that is a list gives one line per its elements), with a
	// final newline; Content-Type application/x-ndjson (Elasticsearch _bulk).
	BodyNDJSON = "ndjson"
)

// effectiveBodyType is body_type, defaulting to json when a body is set, raw
// for a binary upload without a body, else none.
func (o *Operation) effectiveBodyType() string {
	if o.BodyType != "" {
		return o.BodyType
	}
	if o.Body != nil {
		return BodyJSON
	}
	if o.Binary != nil && o.Binary.UploadField != "" {
		return BodyRaw
	}
	return BodyNone
}

// pageParams are the pagination params one page request adds.
type pageParams struct {
	values  map[string]any
	inBody  bool
	inPath  bool              // values go to the path template as page.<param>
	headers map[string]string // header_cursor pagination
	url     string            // absolute next-page URL (link_header, next_url)
}

// built is a request ready to send, with the body bytes SigV4 needs.
type built struct {
	req  *http.Request
	body []byte
}

// buildRequest builds op's request for one item.
func (r *opRun) buildRequest(ctx context.Context, pp pageParams) (*built, error) {
	if pp.inPath {
		r.scope.Page = pp.values
	}
	sc := r.scope
	base := r.op.BaseURL
	if base == "" {
		base = r.pkg.BaseURL
	}
	b, err := renderText(base, sc)
	if err != nil {
		return nil, r.errf("base_url: %w", err)
	}
	rawURL := pp.url
	if rawURL == "" {
		path, err := renderPath(r.op.Path, sc)
		if err != nil {
			return nil, r.errf("path: %w", err)
		}
		if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
			rawURL = path
		} else {
			rawURL = strings.TrimRight(b, "/") + path
		}
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, r.errf("url %q: %w", rawURL, err)
	}
	// The connection's secret goes only to a host the package names: an
	// absolute URL a param or a response names elsewhere goes without it,
	// and without anything rendered from the connection (authHost).
	authed := r.authHost(u.Hostname(), b)
	if !authed {
		saved := r.scope.Cred
		r.scope.Cred, sc.Cred = nil, nil
		defer func() { r.scope.Cred = saved }()
	}
	if pp.url == "" && r.op.Query != nil {
		qv, err := renderObject(r.op.Query, sc)
		if err != nil {
			return nil, r.errf("query: %w", err)
		}
		q := u.Query()
		r.addValues(q, qv)
		u.RawQuery = q.Encode()
	}
	if len(pp.values) > 0 && !pp.inBody && !pp.inPath {
		q := u.Query()
		for k, v := range pp.values {
			q.Set(k, stringify(v))
		}
		u.RawQuery = q.Encode()
	}

	body, contentType, err := r.buildBody(pp)
	if err != nil {
		return nil, err
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	method := strings.ToUpper(r.op.Method)
	if hasTemplate(r.op.Method) {
		m, err := renderText(r.op.Method, r.scope)
		if err != nil {
			return nil, r.errf("method: %w", err)
		}
		if method = strings.ToUpper(strings.TrimSpace(m)); !validMethods[method] {
			return nil, r.errf("method %q is not GET, POST, PUT, PATCH, DELETE, HEAD or a WebDAV method", m)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return nil, r.errf("request: %w", err)
	}
	for _, hs := range []map[string]string{r.pkg.Headers, r.op.Headers} {
		if err := applyHeaders(req.Header, hs, sc); err != nil {
			return nil, r.errf("%w", err)
		}
	}
	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}
	if req.Header.Get("Accept") == "" && !r.downloads() {
		req.Header.Set("Accept", "application/json")
	}
	for k, v := range pp.headers {
		req.Header.Set(k, v)
	}
	if authed {
		if err := injectAuth(req, r.credDef, sc, body); err != nil {
			return nil, r.errf("%w", err)
		}
	}
	return &built{req: req, body: body}, nil
}

// literalHost finds the hosts written in a URL template, not rendered into
// it ("https://upload.box.com/…", "…then:'https://sync.api.bannerbear.com/…'").
var literalHost = regexp.MustCompile(`https?://([A-Za-z0-9.-]+)(?:[:/'"?#]|$)`)

// authHost reports whether host is one the package names, so the
// connection's secret may go to it: the host of the base URL as rendered
// (base), or one of its subdomains; a host written literally in the
// package's or the op's base URL or the op's path; or one of the
// package's auth_hosts ("*.example.com" for its subdomains).
func (r *opRun) authHost(host, base string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return false
	}
	if bu, err := url.Parse(strings.TrimSpace(base)); err == nil {
		if bh := strings.ToLower(strings.TrimSuffix(bu.Hostname(), ".")); bh != "" && (host == bh || strings.HasSuffix(host, "."+bh)) {
			return true
		}
	}
	for _, t := range []string{r.pkg.BaseURL, r.op.BaseURL, r.op.Path} {
		for _, m := range literalHost.FindAllStringSubmatch(t, -1) {
			if strings.EqualFold(strings.TrimSuffix(m[1], "."), host) {
				return true
			}
		}
	}
	for _, h := range r.pkg.AuthHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if sub, ok := strings.CutPrefix(h, "*."); ok {
			if strings.HasSuffix(host, "."+sub) {
				return true
			}
		} else if host == h {
			return true
		}
	}
	return false
}

// buildBody renders the body for the op's body type.
func (r *opRun) buildBody(pp pageParams) ([]byte, string, error) {
	sc := r.scope
	bt := r.bodyType()
	var obj map[string]any
	if bt == BodyJSON || bt == BodyForm || bt == BodyMultipart {
		v, err := renderValue(r.op.Body, sc)
		if err != nil {
			return nil, "", r.errf("body: %w", err)
		}
		if m, ok := v.(map[string]any); ok {
			obj = m
		} else if v != nil && bt != BodyJSON {
			return nil, "", r.errf("body: a %s body must be an object", bt)
		}
		if c := r.credDef; c != nil && len(c.Inject.Body) > 0 {
			if obj == nil {
				obj = map[string]any{}
			}
			for k, tv := range c.Inject.Body {
				v, err := renderString(tv, sc) // typed: a single expression keeps bools/numbers
				if err != nil {
					return nil, "", r.errf("credential %s: inject.body.%s: %w", c.Platform, k, err)
				}
				if !isEmpty(v) {
					obj[k] = v
				}
			}
		}
		if pp.inBody && len(pp.values) > 0 {
			if obj == nil {
				obj = map[string]any{}
			}
			for k, val := range pp.values {
				setPath(obj, k, val) // "variables.after" nests (GraphQL)
			}
		}
		if bt == BodyJSON {
			var out any = obj
			if obj == nil {
				out = v
			}
			if out == nil {
				out = map[string]any{}
			}
			b, err := json.Marshal(out)
			if err != nil {
				return nil, "", r.errf("body: %w", err)
			}
			return b, "application/json", nil
		}
	}
	switch bt {
	case BodyNone:
		return nil, "", nil
	case BodyNDJSON:
		v, err := renderValue(r.op.Body, sc)
		if err != nil {
			return nil, "", r.errf("body: %w", err)
		}
		b, err := ndjsonLines(v)
		if err != nil {
			return nil, "", r.errf("body: %w", err)
		}
		return b, "application/x-ndjson", nil
	case BodyForm:
		q := url.Values{}
		r.addValues(q, obj)
		return []byte(q.Encode()), "application/x-www-form-urlencoded", nil
	case BodyMultipart:
		return r.multipartBody(obj)
	case BodyRaw:
		if r.uploads() {
			data, err := r.uploadData()
			if err != nil {
				return nil, "", err
			}
			ct, _ := renderText(r.op.Binary.MimeType, sc)
			if ct == "" {
				ct = r.uploadMime()
			}
			return data, ct, nil
		}
		v, err := renderValue(r.op.Body, sc)
		if err != nil {
			return nil, "", r.errf("body: %w", err)
		}
		if v == nil {
			return nil, "", nil
		}
		return []byte(stringify(v)), "text/plain; charset=utf-8", nil
	}
	return nil, "", r.errf("unknown body_type %q", bt)
}

func (r *opRun) multipartBody(obj map[string]any) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		vals := []any{obj[k]}
		if arr, ok := obj[k].([]any); ok {
			vals = arr
		}
		for _, v := range vals {
			if err := w.WriteField(k, stringify(v)); err != nil {
				return nil, "", r.errf("body: %w", err)
			}
		}
	}
	if b := r.op.Binary; r.uploads() && !isEmpty(b.Properties) {
		for i, prop := range r.uploadProperties() {
			one := *r
			one.prop = prop
			one.scope.El = map[string]any{"property": prop, "index": i}
			if err := one.writeFilePart(w, b); err != nil {
				return nil, "", err
			}
		}
	} else if b := r.op.Binary; r.uploads() {
		if err := r.writeFilePart(w, b); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", r.errf("body: %w", err)
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// uploadProperty is the input item's binary property to upload.
func (r *opRun) uploadProperty() string {
	if r.prop != "" {
		return r.prop
	}
	if t := r.op.Binary.Property; t != "" {
		if p, err := renderText(t, r.scope); err == nil && p != "" {
			return p
		}
	}
	prop := stringify(paramAt(r.params, r.op.Binary.UploadField))
	if prop == "" {
		prop = "data"
	}
	return prop
}

func (r *opRun) uploadName() string {
	prop := r.uploadProperty()
	if m, ok := r.item.JSON["binary"].(map[string]any); ok {
		if meta, ok := m[prop].(map[string]any); ok {
			if n, _ := meta["fileName"].(string); n != "" {
				return n
			}
		}
	}
	if n, _ := r.item.JSON["file_name"].(string); n != "" {
		return n
	}
	return prop
}

// uploadData finds the bytes to upload: the item's binary property, else a
// base64 string at item.json[prop] or item.json.binary[prop].data (n8n).
func (r *opRun) uploadData() ([]byte, error) {
	if t := r.op.Binary.UploadText; t != "" {
		txt, err := renderText(t, r.scope)
		if err != nil {
			return nil, r.errf("binary.upload_text: %w", err)
		}
		if txt != "" {
			return []byte(txt), nil
		}
	}
	prop := r.uploadProperty()
	if b, ok := r.item.Binary[prop]; ok {
		return b, nil
	}
	if s, ok := r.item.JSON[prop].(string); ok && s != "" {
		if b, err := base64.StdEncoding.DecodeString(s); err == nil {
			return b, nil
		}
	}
	if m, ok := r.item.JSON["binary"].(map[string]any); ok {
		if meta, ok := m[prop].(map[string]any); ok {
			if s, _ := meta["data"].(string); s != "" {
				if b, err := base64.StdEncoding.DecodeString(s); err == nil {
					return b, nil
				}
			}
		}
	}
	return nil, r.errf("field %q: the input item has no binary property %q to upload", r.op.Binary.UploadField, prop)
}
