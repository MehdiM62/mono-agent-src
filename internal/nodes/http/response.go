package httpnodes

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// responseFormats are the response_format values http.request accepts.
var responseFormats = []string{"json", "text", "binary", "file", "autodetect"}

var responseFormatAliases = map[string]string{"string": "text"}

// responseOptions decide how a response becomes output items.
type responseOptions struct {
	format string
	// fullResponse (the default) emits {status, headers, body}; off, the
	// body itself is the item (n8n's default shape).
	fullResponse     bool
	neverError       bool
	outputProperty   string
	lowercaseHeaders bool
}

func parseResponseOptions(config map[string]interface{}, node string) (responseOptions, error) {
	ro := responseOptions{outputProperty: "data"}
	var err error
	wrap := func(err error) error { return fmt.Errorf("%s: %w", node, err) }
	if ro.format, err = oneOf(config, "response_format", "json", responseFormats, responseFormatAliases); err != nil {
		return ro, wrap(err)
	}
	if ro.fullResponse, err = configBool(config, "full_response", true); err != nil {
		return ro, wrap(err)
	}
	if ro.neverError, err = configBool(config, "never_error", false); err != nil {
		return ro, wrap(err)
	}
	if ro.lowercaseHeaders, err = configBool(config, "lowercase_headers", false); err != nil {
		return ro, wrap(err)
	}
	if p := strings.TrimSpace(configString(config, "output_property")); p != "" {
		ro.outputProperty = p
	}
	return ro, nil
}

// effectiveFormat resolves autodetect from the Content-Type.
func effectiveFormat(format string, h http.Header) string {
	if format != "autodetect" {
		return format
	}
	mt, _, _ := mime.ParseMediaType(h.Get("Content-Type"))
	switch {
	case mt == "application/json" || strings.HasSuffix(mt, "+json"):
		return "json"
	case mt == "" || strings.HasPrefix(mt, "text/") || strings.Contains(mt, "xml") ||
		strings.Contains(mt, "javascript") || mt == "application/x-www-form-urlencoded":
		return "text"
	}
	return "file"
}

// decodeBody turns the raw body into the value the format asks for. A
// JSON body that does not parse falls back to text.
func decodeBody(data []byte, format string) interface{} {
	switch format {
	case "json":
		if len(strings.TrimSpace(string(data))) == 0 {
			return ""
		}
		var v interface{}
		if err := json.Unmarshal(data, &v); err != nil {
			return string(data)
		}
		return v
	case "binary":
		return base64.StdEncoding.EncodeToString(data)
	case "file":
		return nil
	}
	return string(data)
}

func headerMap(h http.Header, lower bool) map[string]interface{} {
	out := make(map[string]interface{}, len(h))
	for k, vals := range h {
		if lower {
			k = strings.ToLower(k)
		}
		if len(vals) == 1 {
			out[k] = vals[0]
			continue
		}
		iv := make([]interface{}, len(vals))
		for i, v := range vals {
			iv[i] = v
		}
		out[k] = iv
	}
	return out
}

// fileMeta describes a response kept as binary data.
func fileMeta(f *fetched) map[string]interface{} {
	name := ""
	if _, params, err := mime.ParseMediaType(f.header.Get("Content-Disposition")); err == nil {
		name = params["filename"]
	}
	if name == "" && f.url != nil {
		name = path.Base(f.url.Path)
		if name == "/" || name == "." {
			name = ""
		}
	}
	mt := f.header.Get("Content-Type")
	if mt == "" {
		mt = http.DetectContentType(f.body)
	}
	return map[string]interface{}{"file_name": name, "mime_type": mt, "file_size": len(f.body)}
}

func isSuccess(status int) bool { return status >= 200 && status < 300 }

// shapeResponse turns a response into output items. failed is true for a
// non-2xx status without never_error: the item then carries status,
// headers and body for the error handle whatever full_response says.
func shapeResponse(f *fetched, ro responseOptions) (items []workflow.Item, failed bool) {
	format := effectiveFormat(ro.format, f.header)
	body := decodeBody(f.body, format)
	headers := headerMap(f.header, ro.lowercaseHeaders)

	var binary map[string][]byte
	if format == "file" {
		body = fileMeta(f)
		binary = map[string][]byte{ro.outputProperty: f.body}
	}

	if !isSuccess(f.status) && !ro.neverError {
		if format == "file" {
			body = string(f.body)
		}
		return []workflow.Item{workflow.NewItem(map[string]interface{}{"status": f.status, "headers": headers, "body": body})}, true
	}
	if ro.fullResponse {
		return []workflow.Item{{JSON: map[string]interface{}{"status": f.status, "headers": headers, "body": body}, Binary: binary}}, false
	}
	if format == "file" {
		return []workflow.Item{{JSON: body.(map[string]interface{}), Binary: binary}}, false
	}
	return bodyItems(body, ro.outputProperty), false
}

// bodyItems is n8n's body-only output: an object is the item, a list is one
// item per element, anything else goes under prop.
func bodyItems(body interface{}, prop string) []workflow.Item {
	switch t := body.(type) {
	case map[string]interface{}:
		return []workflow.Item{workflow.NewItem(t)}
	case []interface{}:
		out := make([]workflow.Item, 0, len(t))
		for _, el := range t {
			if m, ok := el.(map[string]interface{}); ok {
				out = append(out, workflow.NewItem(m))
			} else {
				out = append(out, workflow.NewItem(map[string]interface{}{prop: el}))
			}
		}
		return out
	}
	return []workflow.Item{workflow.NewItem(map[string]interface{}{prop: body})}
}
