package apipkg

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

// decodeXML turns an XML document into JSON-like values: an element is
// an object of its children by local name (repeated names become a list),
// attributes as "@name", text as "#text" (or the plain string for a leaf).
// WebDAV PROPFIND (d:multistatus/d:response/d:propstat/d:prop) reads as
// res.multistatus.response.
func decodeXML(b []byte) (any, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	type frame struct {
		name string
		obj  map[string]any
		text strings.Builder
	}
	root := &frame{obj: map[string]any{}}
	stack := []*frame{root}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			f := &frame{name: t.Name.Local, obj: map[string]any{}}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
					continue
				}
				f.obj["@"+a.Name.Local] = a.Value
			}
			stack = append(stack, f)
		case xml.CharData:
			stack[len(stack)-1].text.Write(t)
		case xml.EndElement:
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			var val any = f.obj
			txt := strings.TrimSpace(f.text.String())
			switch {
			case len(f.obj) == 0:
				val = txt
			case txt != "":
				f.obj["#text"] = txt
			}
			parent := stack[len(stack)-1].obj
			if prev, ok := parent[f.name]; ok {
				if l, isList := prev.([]any); isList {
					parent[f.name] = append(l, val)
				} else {
					parent[f.name] = []any{prev, val}
				}
			} else {
				parent[f.name] = val
			}
		}
	}
	return root.obj, nil
}
