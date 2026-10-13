package data

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/unicode"

	"github.com/monoes/mono-agent/internal/workflow"
)

// Helpers shared by the file nodes (convert_to_file, extract_from_file,
// read_write_file, ical, compression's compress/decompress).

// ── text encodings ────────────────────────────────────────────────────────────

// textCodec is a resolved character set: enc is nil for UTF-8 (no
// transcoding), bom is the byte-order mark the set uses, if any.
type textCodec struct {
	name  string
	enc   encoding.Encoding
	bom   []byte
	ascii bool
}

var (
	bomUTF8    = []byte{0xEF, 0xBB, 0xBF}
	bomUTF16LE = []byte{0xFF, 0xFE}
	bomUTF16BE = []byte{0xFE, 0xFF}
)

// lookupCodec resolves an encoding name as n8n spells them (utf8, ucs2,
// utf16le, latin1, ascii, …) or any WHATWG / IANA label (windows-1252,
// shift_jis, iso-8859-2, …).
func lookupCodec(name string) (textCodec, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "", "utf8", "utf-8", "cesu8":
		return textCodec{name: "utf8", bom: bomUTF8}, nil
	case "utf16le", "utf-16le", "ucs2", "ucs-2":
		return textCodec{name: "utf16le", enc: unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM), bom: bomUTF16LE}, nil
	case "utf16be", "utf-16be":
		return textCodec{name: "utf16be", enc: unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM), bom: bomUTF16BE}, nil
	case "latin1", "binary", "iso-8859-1", "iso88591", "l1":
		return textCodec{name: "latin1", enc: charmap.ISO8859_1}, nil
	case "ascii", "us-ascii":
		return textCodec{name: "ascii", enc: charmap.ISO8859_1, ascii: true}, nil
	}
	if e, err := htmlindex.Get(n); err == nil {
		return textCodec{name: n, enc: e}, nil
	}
	if e, err := ianaindex.IANA.Encoding(n); err == nil && e != nil {
		return textCodec{name: n, enc: e}, nil
	}
	return textCodec{}, fmt.Errorf("unsupported encoding %q", name)
}

// encodeText renders s in the named encoding, optionally prefixed with the
// encoding's byte-order mark. Characters the encoding cannot represent are
// replaced rather than failing the run.
func encodeText(s, enc string, addBOM bool) ([]byte, error) {
	c, err := lookupCodec(enc)
	if err != nil {
		return nil, err
	}
	var out []byte
	switch {
	case c.ascii:
		var b bytes.Buffer
		for _, r := range s {
			if r > 0x7f {
				r = '?'
			}
			b.WriteByte(byte(r))
		}
		out = b.Bytes()
	case c.enc == nil:
		out = []byte(s)
	default:
		out, err = encoding.ReplaceUnsupported(c.enc.NewEncoder()).Bytes([]byte(s))
		if err != nil {
			return nil, err
		}
	}
	if addBOM && len(c.bom) > 0 {
		out = append(append([]byte{}, c.bom...), out...)
	}
	return out, nil
}

// decodeText turns b into a string using the named encoding. A leading
// byte-order mark is removed when stripBOM is set; a UTF-16 BOM also wins
// over a UTF-8 default, since a file that starts with one is UTF-16.
func decodeText(b []byte, enc string, stripBOM bool) (string, error) {
	c, err := lookupCodec(enc)
	if err != nil {
		return "", err
	}
	if c.name == "utf8" && strings.TrimSpace(enc) == "" {
		switch {
		case bytes.HasPrefix(b, bomUTF16LE):
			c, _ = lookupCodec("utf16le")
		case bytes.HasPrefix(b, bomUTF16BE):
			c, _ = lookupCodec("utf16be")
		}
	}
	if stripBOM && len(c.bom) > 0 && bytes.HasPrefix(b, c.bom) {
		b = b[len(c.bom):]
	}
	if c.enc == nil {
		return string(b), nil
	}
	out, err := c.enc.NewDecoder().Bytes(b)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ── items ─────────────────────────────────────────────────────────────────────

// newFileItem builds an item that carries one file as binary property prop.
func newFileItem(prop string, data []byte, meta binaryMeta) workflow.Item {
	j := map[string]interface{}{}
	setBinary(j, prop, data, meta)
	return workflow.NewItem(j)
}

// copyBinaryMap copies an item's Binary map so removing a key from the copy
// never reaches the input item.
func copyBinaryMap(m map[string][]byte) map[string][]byte {
	if m == nil {
		return nil
	}
	out := make(map[string][]byte, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// cloneItem deep-copies an item's JSON and shallow-copies its Binary map.
func cloneItem(item workflow.Item) workflow.Item {
	return workflow.Item{JSON: deepCopyMap(item.JSON), Binary: copyBinaryMap(item.Binary)}
}

// putBinaries stores files on j: the flat single-file shape when it is the
// only file the item carries, otherwise one "files" entry per property.
func putBinaries(j map[string]interface{}, props []string, data [][]byte, metas []binaryMeta) {
	_, hasList := j["files"]
	if len(props) == 1 && !hasList {
		setBinary(j, props[0], data[0], metas[0])
		return
	}
	for i, p := range props {
		addBinaryFile(j, p, data[i], metas[i])
	}
}

// inputItemsOrOne returns the input items, or a single empty item when there
// are none, for nodes that do their work once even without input (reading
// files, building an event).
func inputItemsOrOne(items []workflow.Item) []workflow.Item {
	if len(items) == 0 {
		return []workflow.Item{workflow.NewItem(map[string]interface{}{})}
	}
	return items
}

func mainOutput(items []workflow.Item) []workflow.NodeOutput {
	return []workflow.NodeOutput{{Handle: "main", Items: items}}
}

// resolveItemConfig resolves the per-item fields of base against one item's
// JSON (the engine resolved everything else once, against the first item).
// base itself is never mutated.
func resolveItemConfig(engine *workflow.ExpressionEngine, base map[string]interface{}, fields []string, item workflow.Item, input workflow.NodeInput) (map[string]interface{}, error) {
	overlay := map[string]interface{}{}
	for _, k := range fields {
		if v, ok := base[k]; ok {
			overlay[k] = v
		}
	}
	out := make(map[string]interface{}, len(base))
	for k, v := range base {
		out[k] = v
	}
	if len(overlay) == 0 {
		return out, nil
	}
	resolved, err := engine.ResolveConfig(overlay, workflow.ExpressionContext{
		JSON:        item.JSON,
		Node:        input.NodeOutputs,
		WorkflowID:  input.WorkflowID,
		ExecutionID: input.ExecutionID,
	})
	if err != nil {
		return nil, err
	}
	for k, v := range resolved {
		out[k] = v
	}
	return out, nil
}

// keepSourceMode validates the keep_source option (json | binary | both).
func keepSourceMode(config map[string]interface{}) (string, error) {
	switch v := cfgString(config, "keep_source", "json"); v {
	case "json", "binary", "both":
		return v, nil
	default:
		return "", fmt.Errorf("keep_source: %q is not one of json, binary, both", v)
	}
}
