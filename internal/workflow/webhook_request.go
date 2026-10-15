package workflow

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

// defaultWebhookMaxBody is the request body cap when the node sets no
// max_body_bytes; maxWebhookMaxBody bounds what the option may raise it to.
const (
	defaultWebhookMaxBody = 1 << 20
	maxWebhookMaxBody     = 32 << 20
)

// buildWebhookItem turns a webhook request into the run's trigger item.
//
// The item carries the request the way n8n's Webhook node does:
//
//	body     the parsed body: a JSON value (object, array, string, ...), the
//	         fields of a urlencoded or multipart form, or text/* as a string
//	query    the query string (a repeated key holds a list)
//	headers  the request headers, keys lower-cased. Credentials are left
//	         out (the auth_header the webhook checked, Authorization,
//	         Proxy-Authorization, Cookie): trigger data is stored in the
//	         run's history unredacted
//	method   the HTTP method
//	path     the webhook path
//	binary   multipart files: field -> {file_name, mime_type, file_size,
//	         data (base64)}; the bytes are also on Item.Binary
//
// For compatibility with workflows written before these keys existed, when
// the body is an object (JSON or form) its fields are also copied to the top
// level, so $json.<field> keeps working. A body field named like one of the
// keys above wins at the top level, except "body", which always holds the
// whole body.
func buildWebhookItem(r *http.Request, body []byte, pathKey, authHeader string) (Item, error) {
	parsed, files, err := parseWebhookBody(r.Header.Get("Content-Type"), body)
	if err != nil {
		return Item{}, err
	}
	data := make(map[string]interface{})
	if obj, ok := parsed.(map[string]interface{}); ok {
		// The run's chain comes only from a verified signed header, never
		// from the body: whatever the body put under the reserved key is
		// dropped.
		delete(obj, WebhookTraceKey)
		for k, v := range obj {
			data[k] = v
		}
	}
	meta := map[string]interface{}{
		"query":   flattenValues(r.URL.Query()),
		"headers": webhookHeaders(r.Header, authHeader),
		"method":  r.Method,
		"path":    pathKey,
	}
	item := Item{}
	if len(files) > 0 {
		bin := make(map[string]interface{}, len(files))
		item.Binary = make(map[string][]byte, len(files))
		for _, f := range files {
			bin[f.field] = map[string]interface{}{
				"file_name": f.name,
				"mime_type": f.mimeType,
				"file_size": len(f.data),
				"data":      base64.StdEncoding.EncodeToString(f.data),
			}
			item.Binary[f.field] = f.data
		}
		meta["binary"] = bin
	}
	for k, v := range meta {
		if _, taken := data[k]; !taken {
			data[k] = v
		}
	}
	data["body"] = parsed
	item.JSON = data
	return item, nil
}

type webhookFile struct {
	field, name, mimeType string
	data                  []byte
}

// parseWebhookBody decodes body by its content type: urlencoded and
// multipart forms, text/* as a string, anything else as JSON. An empty body
// is an empty object.
func parseWebhookBody(contentType string, body []byte) (interface{}, []webhookFile, error) {
	if len(body) == 0 {
		return map[string]interface{}{}, nil, nil
	}
	mediaType, params, _ := mime.ParseMediaType(contentType)
	switch {
	case mediaType == "application/x-www-form-urlencoded":
		vals, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, nil, fmt.Errorf("invalid form body: %w", err)
		}
		return flattenValues(vals), nil, nil
	case mediaType == "multipart/form-data":
		return parseMultipartBody(body, params["boundary"])
	case strings.HasPrefix(mediaType, "text/"):
		return string(body), nil, nil
	default:
		// JSON, or any other type: the body was always read as JSON, whatever
		// the sender called it, and still is.
		var v interface{}
		if err := json.Unmarshal(body, &v); err != nil {
			return nil, nil, fmt.Errorf("invalid JSON body: %s", err.Error())
		}
		if v == nil {
			// `null`: the run still gets an empty object, as before.
			v = map[string]interface{}{}
		}
		return v, nil, nil
	}
}

// parseMultipartBody reads the form fields and files of a multipart body.
// The body is already bounded by the webhook's size cap and held in memory.
func parseMultipartBody(body []byte, boundary string) (interface{}, []webhookFile, error) {
	if boundary == "" {
		return nil, nil, fmt.Errorf("invalid multipart body: no boundary")
	}
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	fields := make(map[string][]string)
	var files []webhookFile
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("invalid multipart body: %w", err)
		}
		data, err := io.ReadAll(part)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid multipart body: %w", err)
		}
		name := part.FormName()
		if name == "" {
			continue
		}
		if part.FileName() == "" {
			fields[name] = append(fields[name], string(data))
			continue
		}
		mt := part.Header.Get("Content-Type")
		if mt == "" {
			mt = "application/octet-stream"
		}
		files = append(files, webhookFile{field: name, name: part.FileName(), mimeType: mt, data: data})
	}
	return flattenValues(fields), files, nil
}

// flattenValues maps a url.Values-shaped map to item JSON: a key given once
// holds its string, a repeated key a list of strings.
func flattenValues(vals map[string][]string) map[string]interface{} {
	out := make(map[string]interface{}, len(vals))
	for k, vs := range vals {
		if len(vs) == 1 {
			out[k] = vs[0]
			continue
		}
		list := make([]interface{}, len(vs))
		for i, v := range vs {
			list[i] = v
		}
		out[k] = list
	}
	return out
}

// credentialHeaders are never put on a webhook's item.
var credentialHeaders = map[string]bool{"authorization": true, "proxy-authorization": true, "cookie": true}

// webhookHeaders lower-cases header names and joins repeated values, leaving
// out credentials: the header that carried the webhook's auth token and
// credentialHeaders.
func webhookHeaders(h http.Header, authHeader string) map[string]interface{} {
	skip := strings.ToLower(authHeader)
	out := make(map[string]interface{}, len(h))
	for k, vs := range h {
		lk := strings.ToLower(k)
		if credentialHeaders[lk] || (skip != "" && lk == skip) {
			continue
		}
		out[lk] = strings.Join(vs, ", ")
	}
	return out
}
