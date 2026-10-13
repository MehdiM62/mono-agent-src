package data

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// Binary (file) data on items.
//
// mono-agent nodes pass file contents between nodes as a base64 string in an
// item JSON field (http.request's binary response, data.compression,
// data.write_binary_file all use this), so a "binary property" here is the
// name of that JSON field — n8n's binaryPropertyName maps onto it, default
// "data". File metadata travels next to it:
//
//   - one file:  flat keys file_name, mime_type, file_extension, file_size
//   - several:   a "files" list of {property, file_name, mime_type,
//     file_extension, file_size}, one entry per binary property
//
// Readers also accept bytes in workflow.Item.Binary[prop] (google_drive's
// download), and an n8n-shaped object {data, fileName, mimeType} in the JSON
// field (pinned data imported from n8n).

// maxBinaryBytes caps a single file a node reads into memory.
const maxBinaryBytes = 256 << 20 // 256 MiB

const defaultBinaryProperty = "data"

// binaryMeta describes one file carried on an item.
type binaryMeta struct {
	FileName  string
	MimeType  string
	Extension string
}

// readBinary returns the bytes and metadata of binary property prop.
func readBinary(item workflow.Item, prop string) ([]byte, binaryMeta, error) {
	if prop == "" {
		prop = defaultBinaryProperty
	}
	meta := lookupMeta(item.JSON, prop)
	if b, ok := item.Binary[prop]; ok {
		return b, meta, nil
	}
	raw, ok := getPath(item.JSON, prop, false)
	if !ok || raw == nil {
		return nil, meta, fmt.Errorf("binary property %q not found on the item", prop)
	}
	switch v := raw.(type) {
	case string:
		b, err := decodeBase64Loose(v)
		if err != nil {
			return nil, meta, fmt.Errorf("binary property %q is not base64: %w", prop, err)
		}
		return b, meta, nil
	case map[string]interface{}:
		s, _ := v["data"].(string)
		b, err := decodeBase64Loose(s)
		if err != nil {
			return nil, meta, fmt.Errorf("binary property %q: data is not base64: %w", prop, err)
		}
		if fn, _ := v["fileName"].(string); fn != "" {
			meta.FileName = fn
		}
		if mt, _ := v["mimeType"].(string); mt != "" {
			meta.MimeType = mt
		}
		if ext, _ := v["fileExtension"].(string); ext != "" {
			meta.Extension = ext
		}
		return b, meta, nil
	case []byte:
		return v, meta, nil
	default:
		return nil, meta, fmt.Errorf("binary property %q holds %T, want a base64 string", prop, raw)
	}
}

// decodeBase64Loose decodes standard, URL-safe, padded or raw base64, and
// strips a data: URL prefix.
func decodeBase64Loose(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "data:") {
		if i := strings.Index(s, ","); i >= 0 {
			s = s[i+1:]
		}
	}
	s = strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	_, err := base64.StdEncoding.DecodeString(s)
	return nil, err
}

func lookupMeta(j map[string]interface{}, prop string) binaryMeta {
	if files, ok := j["files"].([]interface{}); ok {
		for _, f := range files {
			if m, ok := f.(map[string]interface{}); ok && m["property"] == prop {
				return binaryMeta{
					FileName:  str(m["file_name"]),
					MimeType:  str(m["mime_type"]),
					Extension: str(m["file_extension"]),
				}
			}
		}
	}
	return binaryMeta{
		FileName:  str(j["file_name"]),
		MimeType:  str(j["mime_type"]),
		Extension: str(j["file_extension"]),
	}
}

func str(v interface{}) string {
	s, _ := v.(string)
	return s
}

// completeMeta fills in a missing mime type / extension from the file name
// or the content.
func completeMeta(meta binaryMeta, data []byte) binaryMeta {
	if meta.Extension == "" && meta.FileName != "" {
		meta.Extension = strings.TrimPrefix(filepath.Ext(meta.FileName), ".")
	}
	if meta.MimeType == "" && meta.Extension != "" {
		meta.MimeType = strings.Split(mime.TypeByExtension("."+meta.Extension), ";")[0]
	}
	if meta.MimeType == "" && len(data) > 0 {
		meta.MimeType = strings.Split(http.DetectContentType(data), ";")[0]
	}
	if meta.MimeType == "" {
		meta.MimeType = "application/octet-stream"
	}
	if meta.Extension == "" {
		if exts, _ := mime.ExtensionsByType(meta.MimeType); len(exts) > 0 {
			meta.Extension = strings.TrimPrefix(exts[0], ".")
		}
	}
	return meta
}

// setBinary stores data as binary property prop with flat metadata keys —
// the single-file shape.
func setBinary(j map[string]interface{}, prop string, data []byte, meta binaryMeta) {
	if prop == "" {
		prop = defaultBinaryProperty
	}
	meta = completeMeta(meta, data)
	setPath(j, prop, base64.StdEncoding.EncodeToString(data), false)
	j["file_name"] = meta.FileName
	j["mime_type"] = meta.MimeType
	j["file_extension"] = meta.Extension
	j["file_size"] = len(data)
}

// addBinaryFile stores data as binary property prop and records its metadata
// in the "files" list — the several-files shape.
func addBinaryFile(j map[string]interface{}, prop string, data []byte, meta binaryMeta) {
	meta = completeMeta(meta, data)
	j[prop] = base64.StdEncoding.EncodeToString(data)
	files, _ := j["files"].([]interface{})
	j["files"] = append(files, map[string]interface{}{
		"property":       prop,
		"file_name":      meta.FileName,
		"mime_type":      meta.MimeType,
		"file_extension": meta.Extension,
		"file_size":      len(data),
	})
}

// dropBinary removes binary property prop and its metadata from an item.
func dropBinary(item *workflow.Item, prop string) {
	delete(item.Binary, prop)
	unsetPath(item.JSON, prop, false)
	if files, ok := item.JSON["files"].([]interface{}); ok {
		kept := files[:0:0]
		for _, f := range files {
			if m, ok := f.(map[string]interface{}); ok && m["property"] == prop {
				continue
			}
			kept = append(kept, f)
		}
		if len(kept) == 0 {
			delete(item.JSON, "files")
		} else {
			item.JSON["files"] = kept
		}
		return
	}
	for _, k := range []string{"file_name", "mime_type", "file_extension", "file_size"} {
		delete(item.JSON, k)
	}
}

// binaryProperties lists the binary property names an item carries, used
// where n8n iterates "all binary data" (Split Out's $binary, Compression's
// comma list).
func binaryProperties(item workflow.Item) []string {
	var out []string
	seen := map[string]bool{}
	for k := range item.Binary {
		out = append(out, k)
		seen[k] = true
	}
	if files, ok := item.JSON["files"].([]interface{}); ok {
		for _, f := range files {
			if m, ok := f.(map[string]interface{}); ok {
				if p := str(m["property"]); p != "" && !seen[p] {
					out = append(out, p)
					seen[p] = true
				}
			}
		}
		return out
	}
	if _, ok := item.JSON["file_name"]; ok {
		if _, isStr := item.JSON[defaultBinaryProperty].(string); isStr && !seen[defaultBinaryProperty] {
			out = append(out, defaultBinaryProperty)
		}
	}
	return out
}
