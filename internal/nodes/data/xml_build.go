package data

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// json_to_xml: building an XML document from an item, following the rules
// of xml2js's Builder that n8n uses — attrkey holds attributes, charkey
// holds text, arrays repeat the element, and an object with exactly one
// key uses that key as the root element unless root_name is changed.
//
// Go maps have no key order, so attributes and child elements are written
// in sorted key order (attributes first, then text, then children). The
// output is pretty-printed with two-space indentation.

const xmlDeclaration = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`

type xmlElem struct {
	name  string
	attrs [][2]string
	kids  []xmlKid
}

type xmlKid struct {
	text  string
	cdata bool
	elem  *xmlElem
}

func jsonToXML(obj map[string]interface{}, o xmlBuildOptions) (string, error) {
	rootName := o.RootName
	var rootVal interface{} = obj
	if len(obj) == 1 && o.RootName == "root" {
		for k, v := range obj {
			rootName, rootVal = k, v
		}
	}
	root, err := newXMLElem(rootName)
	if err != nil {
		return "", err
	}
	if err := fillXMLElem(root, rootVal, o); err != nil {
		return "", err
	}
	var b strings.Builder
	if !o.Headless {
		b.WriteString(xmlDeclaration)
		b.WriteByte('\n')
	}
	writeXMLElem(&b, root, 0)
	return b.String(), nil
}

func newXMLElem(name string) (*xmlElem, error) {
	if !validXMLName(name) {
		return nil, fmt.Errorf("%q is not a valid XML element name", name)
	}
	return &xmlElem{name: name}, nil
}

// fillXMLElem renders value into el.
func fillXMLElem(el *xmlElem, value interface{}, o xmlBuildOptions) error {
	switch v := value.(type) {
	case nil:
		return nil
	case map[string]interface{}:
		return fillXMLElemFromMap(el, v, o)
	case []interface{}:
		for _, entry := range v {
			m, ok := entry.(map[string]interface{})
			if !ok {
				if err := addXMLText(el, entry, o); err != nil {
					return err
				}
				continue
			}
			for _, k := range sortedKeys(m) {
				if err := addXMLChild(el, k, m[k], o); err != nil {
					return err
				}
			}
		}
		return nil
	default:
		return addXMLText(el, v, o)
	}
}

func fillXMLElemFromMap(el *xmlElem, m map[string]interface{}, o xmlBuildOptions) error {
	if attrs, ok := m[o.AttrKey].(map[string]interface{}); ok {
		for _, name := range sortedKeys(attrs) {
			if attrs[name] == nil {
				continue
			}
			if !validXMLName(name) {
				return fmt.Errorf("%q is not a valid XML attribute name", name)
			}
			val := xmlScalarString(attrs[name])
			if err := checkXMLChars(val); err != nil {
				return fmt.Errorf("attribute %q: %w", name, err)
			}
			el.attrs = append(el.attrs, [2]string{name, val})
		}
	}
	if text, ok := m[o.CharKey]; ok {
		if err := addXMLText(el, text, o); err != nil {
			return err
		}
	}
	for _, k := range sortedKeys(m) {
		if k == o.AttrKey || k == o.CharKey {
			continue
		}
		if err := addXMLChild(el, k, m[k], o); err != nil {
			return err
		}
	}
	return nil
}

// addXMLChild appends <key> elements for value; an array repeats the element.
func addXMLChild(el *xmlElem, key string, value interface{}, o xmlBuildOptions) error {
	entries, isArr := value.([]interface{})
	if !isArr {
		entries = []interface{}{value}
	}
	for _, entry := range entries {
		child, err := newXMLElem(key)
		if err != nil {
			return err
		}
		if err := fillXMLElem(child, entry, o); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		el.kids = append(el.kids, xmlKid{elem: child})
	}
	return nil
}

func addXMLText(el *xmlElem, value interface{}, o xmlBuildOptions) error {
	if value == nil {
		return nil
	}
	s := xmlScalarString(value)
	if err := checkXMLChars(s); err != nil {
		return fmt.Errorf("<%s>: %w", el.name, err)
	}
	cdata := o.CData && strings.ContainsAny(s, "&<>")
	el.kids = append(el.kids, xmlKid{text: s, cdata: cdata})
	return nil
}

func writeXMLElem(b *strings.Builder, el *xmlElem, depth int) {
	indent := strings.Repeat("  ", depth)
	b.WriteString(indent)
	b.WriteByte('<')
	b.WriteString(el.name)
	for _, a := range el.attrs {
		b.WriteByte(' ')
		b.WriteString(a[0])
		b.WriteString(`="`)
		b.WriteString(escapeXMLAttr(a[1]))
		b.WriteByte('"')
	}
	switch {
	case len(el.kids) == 0:
		b.WriteString("/>")
		return
	case len(el.kids) == 1 && el.kids[0].elem == nil:
		b.WriteByte('>')
		writeXMLText(b, el.kids[0])
	default:
		b.WriteByte('>')
		for _, k := range el.kids {
			b.WriteByte('\n')
			if k.elem != nil {
				writeXMLElem(b, k.elem, depth+1)
				continue
			}
			b.WriteString(indent + "  ")
			writeXMLText(b, k)
		}
		b.WriteByte('\n')
		b.WriteString(indent)
	}
	b.WriteString("</")
	b.WriteString(el.name)
	b.WriteByte('>')
}

func writeXMLText(b *strings.Builder, k xmlKid) {
	if k.cdata {
		b.WriteString("<![CDATA[")
		b.WriteString(strings.ReplaceAll(k.text, "]]>", "]]]]><![CDATA[>"))
		b.WriteString("]]>")
		return
	}
	b.WriteString(escapeXMLText(k.text))
}

var (
	xmlTextEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#xD;")
	xmlAttrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", `"`, "&quot;", "\t", "&#x9;", "\n", "&#xA;", "\r", "&#xD;")
)

func escapeXMLText(s string) string { return xmlTextEscaper.Replace(s) }
func escapeXMLAttr(s string) string { return xmlAttrEscaper.Replace(s) }

// checkXMLChars rejects characters XML 1.0 cannot represent at all.
func checkXMLChars(s string) error {
	for i, r := range s {
		if r == utf8.RuneError {
			if _, size := utf8.DecodeRuneInString(s[i:]); size == 1 {
				return fmt.Errorf("invalid UTF-8 at byte %d", i)
			}
		}
		if r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || (r >= 0x10000 && r <= 0x10FFFF) {
			continue
		}
		return fmt.Errorf("character U+%04X is not allowed in XML", r)
	}
	return nil
}

// validXMLName reports whether name is a legal XML element/attribute name
// (prefixed names such as "soap:Body" included).
func validXMLName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || r == ':' || unicode.IsLetter(r) {
			continue
		}
		if i > 0 && (r == '-' || r == '.' || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) || r == 0xB7) {
			continue
		}
		return false
	}
	return true
}

// xmlScalarString formats a JSON value the way JavaScript's String() would.
func xmlScalarString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return t.String()
	case map[string]interface{}, []interface{}:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(b)
	}
	if f, ok := toFloat(v); ok {
		return jsNumberString(f)
	}
	return fmt.Sprintf("%v", v)
}

// jsNumberString renders a float like JavaScript's Number#toString.
func jsNumberString(f float64) string {
	abs := math.Abs(f)
	if abs != 0 && (abs >= 1e21 || abs < 1e-6) {
		s := strconv.FormatFloat(f, 'e', -1, 64)
		// Go writes e+07 / e-07; JavaScript writes e+7 / e-7.
		if i := strings.IndexAny(s, "e"); i >= 0 && i+2 < len(s) {
			exp := strings.TrimLeft(s[i+2:], "0")
			if exp == "" {
				exp = "0"
			}
			s = s[:i+2] + exp
		}
		return s
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
