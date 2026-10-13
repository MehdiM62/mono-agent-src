package data

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// n8n-compatible XML operations: xml_to_json and json_to_xml.
//
// These follow the semantics of the n8n XML node, which delegates to the
// xml2js library: xml_to_json replaces each item with the parsed document,
// json_to_xml replaces each item with {<data_property_name>: "<xml…>"}.
// The older parse/generate operations (xml.go) are unchanged.

// forbiddenXMLKeys are object keys n8n refuses as attribute/character keys
// and renames when they appear as tag or attribute names, because they
// would reach a JavaScript object's prototype. Go maps have no prototype,
// but the same names are refused and renamed so imported workflows behave
// identically.
var forbiddenXMLKeys = map[string]bool{"__proto__": true, "constructor": true, "prototype": true}

type xmlParseOptions struct {
	AttrKey       string
	CharKey       string
	ExplicitArray bool
	ExplicitRoot  bool
	IgnoreAttrs   bool
	MergeAttrs    bool
	Normalize     bool
	NormalizeTags bool
	Trim          bool
}

type xmlBuildOptions struct {
	AttrKey  string
	CharKey  string
	RootName string
	CData    bool
	Headless bool
	// AllowSurrogateChars is accepted for n8n compatibility. Go strings
	// decoded from JSON never contain unpaired surrogates, so it has no effect.
	AllowSurrogateChars bool
}

// xmlKeyOptions reads attrkey/charkey shared by both directions and
// rejects the prototype-polluting names n8n rejects.
func xmlKeyOptions(config map[string]interface{}) (attrKey, charKey string, err error) {
	attrKey = cfgString(config, "attrkey", "$")
	charKey = cfgString(config, "charkey", "_")
	if forbiddenXMLKeys[attrKey] {
		return "", "", fmt.Errorf("%w: attrkey: the value %q is not allowed", workflow.ErrInvalidConfig, attrKey)
	}
	if forbiddenXMLKeys[charKey] {
		return "", "", fmt.Errorf("%w: charkey: the value %q is not allowed", workflow.ErrInvalidConfig, charKey)
	}
	return attrKey, charKey, nil
}

func parseXMLParseOptions(config map[string]interface{}) (xmlParseOptions, error) {
	attrKey, charKey, err := xmlKeyOptions(config)
	if err != nil {
		return xmlParseOptions{}, err
	}
	return xmlParseOptions{
		AttrKey:       attrKey,
		CharKey:       charKey,
		ExplicitArray: cfgBool(config, "explicit_array", false),
		ExplicitRoot:  cfgBool(config, "explicit_root", true),
		IgnoreAttrs:   cfgBool(config, "ignore_attrs", false),
		MergeAttrs:    cfgBool(config, "merge_attrs", true),
		Normalize:     cfgBool(config, "normalize", false),
		NormalizeTags: cfgBool(config, "normalize_tags", false),
		Trim:          cfgBool(config, "trim", false),
	}, nil
}

func parseXMLBuildOptions(config map[string]interface{}) (xmlBuildOptions, error) {
	attrKey, charKey, err := xmlKeyOptions(config)
	if err != nil {
		return xmlBuildOptions{}, err
	}
	return xmlBuildOptions{
		AttrKey:             attrKey,
		CharKey:             charKey,
		RootName:            cfgString(config, "root_name", "root"),
		CData:               cfgBool(config, "cdata", false),
		Headless:            cfgBool(config, "headless", false),
		AllowSurrogateChars: cfgBool(config, "allow_surrogate_chars", false),
	}, nil
}

// executeN8nXML runs xml_to_json / json_to_xml over every input item.
func executeN8nXML(operation string, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	prop := cfgString(config, "data_property_name", "data")
	out := make([]workflow.Item, 0, len(input.Items))
	switch operation {
	case "xml_to_json":
		opts, err := parseXMLParseOptions(config)
		if err != nil {
			return nil, err
		}
		for i, item := range input.Items {
			raw, ok := getPath(item.JSON, prop, false)
			if !ok {
				return nil, fmt.Errorf("data.xml xml_to_json: item %d has no JSON property called %q (data_property_name)", i, prop)
			}
			src, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("data.xml xml_to_json: item %d: property %q holds %T, want an XML string", i, prop, raw)
			}
			parsed, err := xmlToJSON(src, opts)
			if err != nil {
				return nil, fmt.Errorf("data.xml xml_to_json: item %d: %w", i, err)
			}
			obj, isObj := parsed.(map[string]interface{})
			if !isObj {
				// explicit_root=false on a text-only root yields a bare
				// string; keep the item an object.
				obj = map[string]interface{}{prop: parsed}
			}
			out = append(out, workflow.Item{JSON: obj})
		}
	case "json_to_xml":
		opts, err := parseXMLBuildOptions(config)
		if err != nil {
			return nil, err
		}
		for i, item := range input.Items {
			doc, err := jsonToXML(item.JSON, opts)
			if err != nil {
				return nil, fmt.Errorf("data.xml json_to_xml: item %d: %w", i, err)
			}
			out = append(out, workflow.Item{JSON: map[string]interface{}{prop: doc}})
		}
	}
	return []workflow.NodeOutput{{Handle: "main", Items: out}}, nil
}

// xmlFrame is one open element while parsing.
type xmlFrame struct {
	rawName string // name as written, for matching the end tag
	name    string // name after processing (lowercase, sanitized)
	obj     map[string]interface{}
	text    strings.Builder
}

var xmlRunOfSpace = regexp.MustCompile(`\s{2,}`)

// xmlToJSON parses an XML document the way xml2js does with the given
// options: elements become objects, text-only elements become strings,
// repeated child elements become arrays.
func xmlToJSON(src string, o xmlParseOptions) (interface{}, error) {
	dec := xml.NewDecoder(strings.NewReader(src))
	dec.Strict = true
	// The input is already a decoded Go string; a declared encoding in the
	// prolog describes bytes we never see, so pass the text through.
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }

	var stack []*xmlFrame
	var result interface{}
	rootName := ""
	for {
		tok, err := dec.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) == 0 && rootName != "" {
				return nil, fmt.Errorf("invalid XML: more than one root element (<%s> after <%s>)", qualifiedXMLName(t.Name), rootName)
			}
			f := &xmlFrame{rawName: qualifiedXMLName(t.Name), obj: map[string]interface{}{}}
			f.name = sanitizeXMLKey(f.rawName)
			if o.NormalizeTags {
				f.name = sanitizeXMLKey(strings.ToLower(f.rawName))
			}
			if !o.IgnoreAttrs {
				for _, a := range t.Attr {
					key := sanitizeXMLKey(qualifiedXMLName(a.Name))
					if o.MergeAttrs {
						assignOrPush(f.obj, key, a.Value, o.ExplicitArray)
						continue
					}
					attrs, _ := f.obj[o.AttrKey].(map[string]interface{})
					if attrs == nil {
						attrs = map[string]interface{}{}
						f.obj[o.AttrKey] = attrs
					}
					attrs[key] = a.Value
				}
			}
			if len(stack) == 0 {
				rootName = f.rawName
			}
			stack = append(stack, f)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("invalid XML: unexpected end tag </%s>", qualifiedXMLName(t.Name))
			}
			f := stack[len(stack)-1]
			if end := qualifiedXMLName(t.Name); end != f.rawName {
				return nil, fmt.Errorf("invalid XML: element <%s> closed by </%s>", f.rawName, end)
			}
			stack = stack[:len(stack)-1]
			value := closeXMLFrame(f, o)
			if len(stack) > 0 {
				assignOrPush(stack[len(stack)-1].obj, f.name, value, o.ExplicitArray)
				continue
			}
			if o.ExplicitRoot {
				result = map[string]interface{}{f.name: value}
			} else {
				result = value
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text.Write(t)
			}
		}
	}
	if len(stack) > 0 {
		return nil, fmt.Errorf("invalid XML: element <%s> is never closed", stack[len(stack)-1].rawName)
	}
	if result == nil {
		return nil, errors.New("invalid XML: no root element found")
	}
	return result, nil
}

// closeXMLFrame turns a finished element into its JSON value.
func closeXMLFrame(f *xmlFrame, o xmlParseOptions) interface{} {
	text := f.text.String()
	if strings.TrimSpace(text) == "" {
		if len(f.obj) == 0 {
			// Empty or whitespace-only element: xml2js keeps the
			// whitespace as the value.
			return text
		}
		return f.obj
	}
	if o.Trim {
		text = strings.TrimSpace(text)
	}
	if o.Normalize {
		text = strings.TrimSpace(xmlRunOfSpace.ReplaceAllString(text, " "))
	}
	if len(f.obj) == 0 {
		return text
	}
	f.obj[o.CharKey] = text
	return f.obj
}

// assignOrPush stores value under key; a second value for the same key
// turns it into an array. With explicitArray every value is an array.
func assignOrPush(obj map[string]interface{}, key string, value interface{}, explicitArray bool) {
	existing, ok := obj[key]
	if !ok {
		if explicitArray {
			obj[key] = []interface{}{value}
		} else {
			obj[key] = value
		}
		return
	}
	if arr, isArr := existing.([]interface{}); isArr {
		obj[key] = append(arr, value)
		return
	}
	obj[key] = []interface{}{existing, value}
}

func qualifiedXMLName(n xml.Name) string {
	if n.Space == "" {
		return n.Local
	}
	return n.Space + ":" + n.Local
}

// sanitizeXMLKey renames tag and attribute names that would be
// prototype keys in JavaScript, matching n8n's output.
func sanitizeXMLKey(name string) string {
	if forbiddenXMLKeys[name] {
		return "sanitized_" + name
	}
	return name
}
