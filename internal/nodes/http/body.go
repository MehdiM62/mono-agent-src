package httpnodes

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/monoes/mono-agent/internal/fsconfine"
	"github.com/monoes/mono-agent/internal/workflow"
)

// bodyTypes are the body_type values http.request accepts.
var bodyTypes = []string{"none", "json", "form", "multipart", "text", "raw", "binary"}

// bodyTypeAliases maps n8n's contentType names (and common spellings).
var bodyTypeAliases = map[string]string{
	"form-urlencoded":     "form",
	"urlencoded":          "form",
	"multipart-form-data": "multipart",
	"form-data":           "multipart",
	"binaryData":          "binary",
	"file":                "binary",
}

// requestBody is an encoded request body.
type requestBody struct {
	data        []byte
	present     bool
	contentType string
}

// buildBody encodes config["body"] as body_type. overrides (pagination
// body parameters) are set on top of an object body. An empty body_type
// is inferred: an object, list or JSON-looking string is sent as JSON, any
// other string as text — except on GET and HEAD, which then send none.
func buildBody(ctx context.Context, method string, config map[string]interface{}, item workflow.Item, overrides map[string]interface{}) (requestBody, error) {
	bodyType, err := oneOf(config, "body_type", "", bodyTypes, bodyTypeAliases)
	if err != nil {
		return requestBody{}, err
	}
	raw := config["body"]
	if bodyType == "" && (method == http.MethodGet || method == http.MethodHead) && len(overrides) == 0 {
		return requestBody{}, nil
	}
	if bodyType == "" {
		bodyType = inferBodyType(raw)
		if bodyType == "none" && len(overrides) > 0 {
			bodyType = "json"
		}
	}
	if bodyType == "binary" {
		return binaryBody(config, item)
	}
	if bodyType == "none" || (isEmptyBody(raw) && len(overrides) == 0) {
		return requestBody{}, nil
	}

	switch bodyType {
	case "json":
		if len(overrides) > 0 {
			obj, err := bodyObject(raw)
			if err != nil {
				return requestBody{}, err
			}
			for k, v := range overrides {
				obj[k] = v
			}
			raw = obj
		}
		if s, ok := raw.(string); ok {
			return requestBody{data: []byte(s), present: true, contentType: "application/json"}, nil
		}
		b, err := json.Marshal(raw)
		if err != nil {
			return requestBody{}, fmt.Errorf("body: cannot encode as JSON: %v", err)
		}
		return requestBody{data: b, present: true, contentType: "application/json"}, nil
	case "form":
		if s, ok := raw.(string); ok && len(overrides) == 0 && !looksLikeJSON(s) {
			// Already encoded (a=1&b=2): sent as given.
			return requestBody{data: []byte(s), present: true, contentType: "application/x-www-form-urlencoded"}, nil
		}
		fields, err := parseNameValues(raw, "body")
		if err != nil {
			return requestBody{}, err
		}
		form := url.Values{}
		applyQuery(form, fields, "repeat", nil)
		for k, v := range overrides {
			form.Set(k, stringifyValue(v))
		}
		return requestBody{data: []byte(form.Encode()), present: true, contentType: "application/x-www-form-urlencoded"}, nil
	case "multipart":
		return multipartBody(ctx, raw, item, overrides)
	case "text", "raw":
		var data []byte
		if s, ok := raw.(string); ok {
			data = []byte(s)
		} else {
			data, _ = json.Marshal(raw)
		}
		ct := "text/plain"
		if bodyType == "raw" {
			ct = strings.TrimSpace(configString(config, "raw_content_type"))
		}
		return requestBody{data: data, present: true, contentType: ct}, nil
	}
	return requestBody{}, nil
}

func isEmptyBody(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	}
	return false
}

func looksLikeJSON(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}

func inferBodyType(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return "none"
	case string:
		if strings.TrimSpace(t) == "" {
			return "none"
		}
		if looksLikeJSON(t) {
			return "json"
		}
		return "text"
	}
	return "json"
}

// bodyObject reads an object body (a map or a JSON object string) so
// pagination body parameters can be set on it.
func bodyObject(raw interface{}) (map[string]interface{}, error) {
	switch t := raw.(type) {
	case nil:
		return map[string]interface{}{}, nil
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, v := range t {
			out[k] = v
		}
		return out, nil
	case string:
		if strings.TrimSpace(t) == "" {
			return map[string]interface{}{}, nil
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(t), &m); err != nil {
			return nil, fmt.Errorf("body: pagination body parameters need a JSON object body: %v", err)
		}
		return m, nil
	}
	return nil, fmt.Errorf("body: pagination body parameters need an object body, got %T", raw)
}

// binaryBody sends an input item's binary property (binary_property,
// default "data") as the body. A base64 string at that key of the item's
// JSON is accepted too.
func binaryBody(config map[string]interface{}, item workflow.Item) (requestBody, error) {
	prop := strings.TrimSpace(configString(config, "binary_property"))
	if prop == "" {
		prop = "data"
	}
	data, ok := item.Binary[prop]
	if !ok {
		s, isStr := item.JSON[prop].(string)
		if !isStr {
			return requestBody{}, fmt.Errorf("binary_property: the input item has no binary data %q", prop)
		}
		dec, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return requestBody{}, fmt.Errorf("binary_property: item field %q is not base64: %v", prop, err)
		}
		data = dec
	}
	ct := strings.TrimSpace(configString(config, "raw_content_type"))
	if ct == "" {
		ct = http.DetectContentType(data)
	}
	return requestBody{data: data, present: true, contentType: ct}, nil
}

// multipartBody encodes a multipart/form-data body. Each field is a text
// value, or a file:
//   - {"file": "/path", "filename": "...", "content_type": "..."} reads a
//     local file (confined to the org workdir in a granted run);
//   - {"binary_property": "data"} sends the input item's binary data;
//   - in the row form, {"name", "parameter_type": "formBinaryData",
//     "input_data_field_name": "data"} is n8n's spelling of the same.
func multipartBody(ctx context.Context, raw interface{}, item workflow.Item, overrides map[string]interface{}) (requestBody, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	rows, err := multipartRows(raw)
	if err != nil {
		return requestBody{}, err
	}
	for k, v := range overrides {
		rows = append(rows, map[string]interface{}{"name": k, "value": v})
	}
	for i, row := range rows {
		name, _ := row["name"].(string)
		if err := writeMultipartField(ctx, w, name, row, item); err != nil {
			return requestBody{}, fmt.Errorf("body[%d] (%s): %w", i, name, err)
		}
	}
	if err := w.Close(); err != nil {
		return requestBody{}, fmt.Errorf("body: %v", err)
	}
	return requestBody{data: buf.Bytes(), present: true, contentType: w.FormDataContentType()}, nil
}

// multipartRows normalises the multipart body to rows of
// {name, value | file | binary_property ...}.
func multipartRows(raw interface{}) ([]map[string]interface{}, error) {
	if s, ok := raw.(string); ok {
		var decoded interface{}
		if err := json.Unmarshal([]byte(s), &decoded); err != nil {
			return nil, fmt.Errorf("body: multipart needs an object or a list of {name, value}: %v", err)
		}
		raw = decoded
	}
	var rows []map[string]interface{}
	switch t := raw.(type) {
	case map[string]interface{}:
		fields, _ := parseNameValues(t, "body")
		for _, f := range fields {
			row := map[string]interface{}{"name": f.Name}
			if spec, ok := f.Value.(map[string]interface{}); ok {
				for k, v := range spec {
					row[k] = v
				}
			} else {
				row["value"] = f.Value
			}
			rows = append(rows, row)
		}
	case []interface{}:
		for i, el := range t {
			row, ok := el.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("body[%d]: expected an object with name and value, got %T", i, el)
			}
			if n, _ := row["name"].(string); strings.TrimSpace(n) == "" {
				return nil, fmt.Errorf("body[%d]: name is empty", i)
			}
			rows = append(rows, row)
		}
	case nil:
	default:
		return nil, fmt.Errorf("body: multipart needs an object or a list of {name, value}, got %T", raw)
	}
	return rows, nil
}

func writeMultipartField(ctx context.Context, w *multipart.Writer, name string, row map[string]interface{}, item workflow.Item) error {
	str := func(k string) string { s, _ := row[k].(string); return strings.TrimSpace(s) }
	prop := str("binary_property")
	if pt := str("parameter_type"); pt == "formBinaryData" || pt == "binary" {
		if prop = str("input_data_field_name"); prop == "" {
			prop = "data"
		}
	}

	var data []byte
	filename := str("filename")
	switch {
	case str("file") != "":
		p, err := fsconfine.Path(ctx, str("file"))
		if err != nil {
			return err
		}
		if data, err = os.ReadFile(p); err != nil {
			return fmt.Errorf("read file: %w", err)
		}
		if filename == "" {
			filename = filepath.Base(p)
		}
	case prop != "":
		b, ok := item.Binary[prop]
		if !ok {
			return fmt.Errorf("the input item has no binary data %q", prop)
		}
		data = b
		if filename == "" {
			filename = prop
		}
	default:
		return w.WriteField(name, stringifyValue(row["value"]))
	}

	ct := str("content_type")
	if ct == "" {
		ct = http.DetectContentType(data)
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, name, filename))
	h.Set("Content-Type", ct)
	part, err := w.CreatePart(h)
	if err != nil {
		return err
	}
	_, err = part.Write(data)
	return err
}
