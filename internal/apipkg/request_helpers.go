package apipkg

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"path"
	"strings"
)

// renderPath renders a path template, URL-escaping each expression value.
func renderPath(s string, sc Scope) (string, error) {
	if !hasTemplate(s) {
		return s, nil
	}
	t, err := parseTemplate(s)
	if err != nil {
		return "", err
	}
	// A path that is one expression yielding an absolute URL (an upload
	// URL from a before step, hdr.Location) is used as is, unescaped.
	if t.single {
		if v, err := t.parts[0].expr.eval(sc); err == nil {
			if u := stringify(v); strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
				return u, nil
			}
		}
	}
	v, err := t.render(sc, url.PathEscape)
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// addValues adds rendered values to q: arrays repeat the key, objects are
// JSON, everything else is stringified.
func addValues(q url.Values, obj map[string]any) {
	for k, v := range obj {
		if arr, ok := v.([]any); ok {
			for _, el := range arr {
				q.Add(k, stringify(el))
			}
			continue
		}
		q.Set(k, stringify(v))
	}
}

// uploadMime is the upload's MIME type: the binary's own (n8n mimeType or
// mono's mime_type field), else guessed from the file name, else
// application/octet-stream.
func (r *opRun) uploadMime() string {
	if t := r.op.Binary.UploadText; t != "" {
		if txt, _ := renderText(t, r.scope); txt != "" {
			return "text/plain"
		}
	}
	prop := r.uploadProperty()
	if m, ok := r.item.JSON["binary"].(map[string]any); ok {
		if meta, ok := m[prop].(map[string]any); ok {
			if t, _ := meta["mimeType"].(string); t != "" {
				return t
			}
		}
	}
	if t, _ := r.item.JSON["mime_type"].(string); t != "" {
		return t
	}
	if t := mime.TypeByExtension(path.Ext(r.uploadName())); t != "" {
		return t
	}
	return "application/octet-stream"
}

// binMeta describes the upload for the bin template root.
func (r *opRun) binMeta() map[string]any {
	name := r.uploadName()
	m := map[string]any{
		"file_name": name, "mime_type": r.uploadMime(),
		"file_extension": strings.TrimPrefix(path.Ext(name), "."),
	}
	// base64 for JSON-body uploads (GitHub/GitLab file content); a missing
	// binary leaves it unset so the error comes from the op that needs it.
	if data, err := r.uploadData(); err == nil {
		m["base64"] = base64.StdEncoding.EncodeToString(data)
		m["file_size"] = len(data)
	}
	if r.prop == "" && (!isEmpty(r.op.Binary.Properties) || len(r.urlProps) > 0) {
		var files []any
		props := r.uploadProperties()
		if isEmpty(r.op.Binary.Properties) {
			props = nil
		}
		for _, prop := range append(props, r.urlProps...) {
			one := *r
			one.prop = prop
			f := one.binMeta()
			f["property"] = prop
			files = append(files, f)
		}
		m["files"] = files
	}
	return m
}

// uploads reports whether this run uploads the item's binary.
func (r *opRun) uploads() bool {
	b := r.op.Binary
	if b == nil || b.UploadField == "" {
		return false
	}
	if b.UploadWhen == "" {
		return true
	}
	w, err := renderString(b.UploadWhen, r.scope)
	return err == nil && truthy(w)
}

// bodyType is the op's body type for this run: without an upload, an op
// that only uploads sends no body and a multipart op sends JSON.
func (r *opRun) bodyType() string {
	bt := r.op.effectiveBodyType()
	if r.op.Binary == nil || r.op.Binary.UploadWhen == "" || r.uploads() {
		return bt
	}
	if fb := r.op.Binary.FallbackBodyType; fb != "" {
		return fb
	}
	if bt == BodyForm || bt == BodyJSON {
		return bt // an explicit json/form body stays that type
	}
	if r.op.Body != nil {
		return BodyJSON
	}
	return BodyNone
}

// addValues encodes obj into q in the op's query style: "brackets"
// flattens nested objects and lists PHP-style (a[b][0][c]=v, Magento),
// otherwise lists repeat the key and objects are sent as JSON.
func (r *opRun) addValues(q url.Values, obj map[string]any) {
	style := r.op.QueryStyle
	if style == "" {
		style = r.pkg.QueryStyle
	}
	if style != "brackets" {
		addValues(q, obj)
		return
	}
	for k, v := range obj {
		addBrackets(q, k, v)
	}
}

func addBrackets(q url.Values, key string, v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			addBrackets(q, key+"["+k+"]", x)
		}
	case []any:
		for i, x := range t {
			addBrackets(q, fmt.Sprintf("%s[%d]", key, i), x)
		}
	default:
		if !isEmpty(v) {
			q.Set(key, stringify(v))
		}
	}
}

// setPath sets obj[a][b]… = val for the dot path "a.b…", creating
// intermediate objects (a non-object in the way is replaced).
func setPath(obj map[string]any, path string, val any) {
	parts := strings.Split(path, ".")
	cur := obj
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = val
}

// uploadProperties renders binary.properties into property names.
func (r *opRun) uploadProperties() []string {
	v, err := renderValue(r.op.Binary.Properties, r.scope)
	if err != nil {
		return nil
	}
	var out []string
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			if s := strings.TrimSpace(stringify(x)); s != "" {
				out = append(out, s)
			}
		}
	default:
		for _, s := range strings.Split(stringify(v), ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// writeFilePart writes the current upload property as one multipart part.
func (r *opRun) writeFilePart(w *multipart.Writer, b *Binary) error {
	data, err := r.uploadData()
	if err != nil {
		return err
	}
	field, _ := renderText(b.MultipartField, r.scope)
	if field == "" {
		field = "file"
	}
	name, _ := renderText(b.FileName, r.scope)
	if name == "" {
		name = r.uploadName()
	}
	ct, _ := renderText(b.MimeType, r.scope)
	if ct == "" {
		ct = r.uploadMime()
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, name))
	h.Set("Content-Type", ct)
	part, err := w.CreatePart(h)
	if err != nil {
		return r.errf("body: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return r.errf("body: %w", err)
	}
	return nil
}

// ndjsonLines encodes a list as newline-delimited JSON: one line per
// element, a nested list giving one line per its elements (a $map that
// emits an action line and a document line per item).
func ndjsonLines(v any) ([]byte, error) {
	list, ok := v.([]any)
	if !ok {
		if v == nil {
			return nil, fmt.Errorf("an ndjson body must render to a list")
		}
		list = []any{v}
	}
	var buf bytes.Buffer
	for _, el := range list {
		lines := []any{el}
		if inner, ok := el.([]any); ok {
			lines = inner
		}
		for _, l := range lines {
			b, err := json.Marshal(l)
			if err != nil {
				return nil, err
			}
			buf.Write(b)
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes(), nil
}
