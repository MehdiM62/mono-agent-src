package apipkg

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// getFile downloads an http(s) URL without the credential and returns its
// bytes, Content-Type and parsed URL; where names the package key in errors.
func (r *opRun) getFile(ctx context.Context, v, where string) ([]byte, string, *url.URL, error) {
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, "", nil, r.errf("%s: %q is not an http(s) URL", where, v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v, nil)
	if err != nil {
		return nil, "", nil, r.errf("%s: %w", where, err)
	}
	resp, err := r.client().Do(req)
	if err != nil {
		return nil, "", nil, r.errf("%s: GET %s: %w", where, u.Redacted(), err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return nil, "", nil, r.errf("%s: reading %s: %w", where, u.Redacted(), err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", nil, r.errf("%s: GET %s: HTTP %d: %s", where, u.Redacted(), resp.StatusCode, errorBodyEcho(data))
	}
	if len(data) > maxResponseBody {
		return nil, "", nil, r.errf("%s: the file exceeds %d MiB", where, maxResponseBody>>20)
	}
	return data, resp.Header.Get("Content-Type"), u, nil
}

// fetchUploadURLs downloads every URL binary.upload_urls renders (a list,
// or comma-separated text) onto the run's item as binary properties
// "url_0", "url_1", … with their name and MIME type, so bin.files lists
// them after binary.properties' own files.
func (r *opRun) fetchUploadURLs(ctx context.Context) error {
	b := r.op.Binary
	if b == nil || isEmpty(b.UploadURLs) {
		return nil
	}
	v, err := renderValue(b.UploadURLs, r.scope)
	if err != nil {
		return r.errf("binary.upload_urls: %w", err)
	}
	var urls []string
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			if s := strings.TrimSpace(stringify(x)); s != "" {
				urls = append(urls, s)
			}
		}
	default:
		for _, s := range strings.Split(stringify(v), ",") {
			if s = strings.TrimSpace(s); s != "" {
				urls = append(urls, s)
			}
		}
	}
	if len(urls) == 0 {
		return nil
	}
	it := workflow.Item{JSON: make(map[string]any, len(r.item.JSON)+1), Binary: make(map[string][]byte, len(r.item.Binary)+len(urls))}
	for k, x := range r.item.JSON {
		it.JSON[k] = x
	}
	for k, x := range r.item.Binary {
		it.Binary[k] = x
	}
	meta := map[string]any{}
	if old, ok := it.JSON["binary"].(map[string]any); ok {
		for k, x := range old {
			meta[k] = x
		}
	}
	for i, s := range urls {
		data, ct, u, err := r.getFile(ctx, s, fmt.Sprintf("binary.upload_urls[%d]", i))
		if err != nil {
			return err
		}
		prop := fmt.Sprintf("url_%d", i)
		name := path.Base(u.Path)
		if name == "." || name == "/" {
			name = prop
		}
		if mt, _, err := mime.ParseMediaType(ct); err == nil {
			ct = mt
		} else {
			ct = mime.TypeByExtension(path.Ext(name))
		}
		it.Binary[prop] = data
		meta[prop] = map[string]any{"fileName": name, "mimeType": ct}
		r.urlProps = append(r.urlProps, prop)
	}
	it.JSON["binary"] = meta
	r.item = it
	return nil
}
