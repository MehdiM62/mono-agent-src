package data

// XMLNodeSchema documents the config keys XMLNode.Execute reads out of its
// map[string]interface{} config — see internal/nodes/control.SetNodeSchema's
// doc comment for why this is a companion struct rather than the runtime
// config, and internal/tools/schemagen for the tag grammar.
//
// parse/generate are the original operations; xml_to_json/json_to_xml
// follow n8n's XML node (xml2js semantics, see xml_n8n.go). n8n's "options"
// collection is flattened into top-level keys (see N8nXmlParams).
type XMLNodeSchema struct {
	Operation string `json:"operation" schema:"label=Operation,type=select,required,options=parse|generate|xml_to_json|json_to_xml,default=parse,help=parse/generate keep the item and write one field. xml_to_json replaces the item with the parsed document; json_to_xml replaces it with {data_property_name: xml}."`

	Field string `json:"field" schema:"label=XML Field,type=text,required,placeholder=xml_content,depends_on_key=operation,depends_on_values=parse|generate"`

	OutputField string `json:"output_field" schema:"label=Output Field,type=text,help=Defaults to the XML field， overwriting it.,depends_on_key=operation,depends_on_values=parse|generate"`

	RootElement string `json:"root_element" schema:"label=Root Element Name,type=text,default=root,depends_on_key=operation,depends_on_values=generate"`

	DataPropertyName string `json:"data_property_name" schema:"label=Property Name,type=text,default=data,help=xml_to_json: the item property holding the XML. json_to_xml: the property the XML is written to.,depends_on_key=operation,depends_on_values=xml_to_json|json_to_xml"`

	AttrKey string `json:"attrkey" schema:"label=Attribute Key,type=text,default=$,help=Key that holds an element's attributes (when not merged).,depends_on_key=operation,depends_on_values=xml_to_json|json_to_xml"`

	CharKey string `json:"charkey" schema:"label=Character Key,type=text,default=_,help=Key that holds an element's text when it also has attributes or children.,depends_on_key=operation,depends_on_values=xml_to_json|json_to_xml"`

	ExplicitArray bool `json:"explicit_array" schema:"label=Explicit Array,type=boolean,default=false,help=Always put child elements in an array (otherwise only repeated elements become arrays).,depends_on_key=operation,depends_on_values=xml_to_json"`

	ExplicitRoot bool `json:"explicit_root" schema:"label=Explicit Root,type=boolean,default=true,help=Keep the root element as the top-level key.,depends_on_key=operation,depends_on_values=xml_to_json"`

	IgnoreAttrs bool `json:"ignore_attrs" schema:"label=Ignore Attributes,type=boolean,default=false,help=Drop all XML attributes.,depends_on_key=operation,depends_on_values=xml_to_json"`

	MergeAttrs bool `json:"merge_attrs" schema:"label=Merge Attributes,type=boolean,default=true,help=Put attributes next to child elements instead of under the attribute key. Ignored when attributes are ignored.,depends_on_key=operation,depends_on_values=xml_to_json"`

	Normalize bool `json:"normalize" schema:"label=Normalize,type=boolean,default=false,help=Collapse runs of whitespace inside text and trim it.,depends_on_key=operation,depends_on_values=xml_to_json"`

	NormalizeTags bool `json:"normalize_tags" schema:"label=Normalize Tags,type=boolean,default=false,help=Lower-case all tag names.,depends_on_key=operation,depends_on_values=xml_to_json"`

	Trim bool `json:"trim" schema:"label=Trim,type=boolean,default=false,help=Trim whitespace at the start and end of text.,depends_on_key=operation,depends_on_values=xml_to_json"`

	RootName string `json:"root_name" schema:"label=Root Name,type=text,default=root,help=Root element name. With the default and an item that has exactly one key， that key becomes the root.,depends_on_key=operation,depends_on_values=json_to_xml"`

	Headless bool `json:"headless" schema:"label=Headless,type=boolean,default=false,help=Omit the <?xml ...?> declaration.,depends_on_key=operation,depends_on_values=json_to_xml"`

	CData bool `json:"cdata" schema:"label=CDATA,type=boolean,default=false,help=Wrap text containing & < or > in CDATA sections instead of escaping it.,depends_on_key=operation,depends_on_values=json_to_xml"`

	AllowSurrogateChars bool `json:"allow_surrogate_chars" schema:"label=Allow Surrogate Chars,type=boolean,default=false,help=Accepted for n8n compatibility; has no effect.,depends_on_key=operation,depends_on_values=json_to_xml"`
}
