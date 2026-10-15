package data

// HTMLNodeSchema documents the config keys HTMLNode.Execute reads out of its
// map[string]interface{} config — see internal/nodes/control.SetNodeSchema's
// doc comment for why this is a companion struct rather than the runtime
// config, and internal/tools/schemagen for the tag grammar.
//
// extract/extract_all/text/generate are the original operations (see
// schema_keys_test.go's TestHTMLNodeUsesCodeOperationNames);
// extract_html_content/generate_html_template/convert_to_html_table follow
// n8n's HTML node v1.2 (html_n8n.go, html_table.go). n8n's "options"
// collections are flattened into top-level keys (see N8nHtmlParams).
type HTMLNodeSchema struct {
	Operation string `json:"operation" schema:"label=Operation,type=select,required,options=extract|extract_all|text|generate|extract_html_content|generate_html_template|convert_to_html_table,default=extract"`

	Field string `json:"field" schema:"label=HTML Field,type=text,required,placeholder=html_content,depends_on_key=operation,depends_on_values=extract|extract_all|text"`

	Selector string `json:"selector" schema:"label=CSS Selector,type=text,placeholder=div.article p,depends_on_key=operation,depends_on_values=extract|extract_all"`

	Attribute string `json:"attribute" schema:"label=Attribute (optional),type=text,placeholder=href,help=Extract this HTML attribute instead of text content.,depends_on_key=operation,depends_on_values=extract|extract_all"`

	OutputField string `json:"output_field" schema:"label=Output Field,type=text,help=Defaults to the HTML field， overwriting it.,depends_on_key=operation,depends_on_values=extract|extract_all|text|generate"`

	Template string `json:"template" schema:"label=HTML Template,type=textarea,rows=5,placeholder=<div>{{.title}}</div>,depends_on_key=operation,depends_on_values=generate"`

	HTML string `json:"html" schema:"label=HTML Template,type=code,language=html,rows=10,placeholder=<h1>{{ $json.title }}</h1>,help=Evaluated once per item; {{ }} expressions read that item. The output item is {html: ...}.,depends_on_key=operation,depends_on_values=generate_html_template"`

	SourceData string `json:"source_data" schema:"label=Source Data,type=select,options=json|binary,default=json,help=Read the HTML from a JSON property or from a binary (base64) property.,depends_on_key=operation,depends_on_values=extract_html_content"`

	DataPropertyName string `json:"data_property_name" schema:"label=Property Name,type=text,default=data,help=JSON: dot path to a string or a list of strings (one output item per string). Binary: the binary property name.,depends_on_key=operation,depends_on_values=extract_html_content"`

	ExtractionValues string `json:"extraction_values" schema:"label=Extraction Values (JSON),type=code,language=json,rows=6,placeholder=[{\"key\": \"title\"， \"css_selector\": \"h1\"， \"return_value\": \"text\"}],help=List of {key， css_selector， return_value (text｜html｜attribute｜value)， attribute， return_array， skip_selectors}. Each output item holds only these keys.,depends_on_key=operation,depends_on_values=extract_html_content"`

	TrimValues bool `json:"trim_values" schema:"label=Trim Values,type=boolean,default=false,help=Remove whitespace at the start and end of each value.,depends_on_key=operation,depends_on_values=extract_html_content"`

	CleanUpText bool `json:"clean_up_text" schema:"label=Clean Up Text,type=boolean,default=false,help=Trim each value and collapse line breaks and runs of whitespace into single spaces.,depends_on_key=operation,depends_on_values=extract_html_content"`

	Capitalize bool `json:"capitalize" schema:"label=Capitalize Headers,type=boolean,default=false,help=Turn first_name into First Name in the header row.,depends_on_key=operation,depends_on_values=convert_to_html_table"`

	CustomStyling bool `json:"custom_styling" schema:"label=Custom Styling,type=boolean,default=false,help=Leave out the built-in inline styles.,depends_on_key=operation,depends_on_values=convert_to_html_table"`

	Caption string `json:"caption" schema:"label=Caption,type=text,depends_on_key=operation,depends_on_values=convert_to_html_table"`

	TableAttributes string `json:"table_attributes" schema:"label=Table Attributes,type=text,placeholder=style=\"padding:10px\",depends_on_key=operation,depends_on_values=convert_to_html_table"`

	HeaderAttributes string `json:"header_attributes" schema:"label=Header Attributes,type=text,placeholder=style=\"padding:10px\",depends_on_key=operation,depends_on_values=convert_to_html_table"`

	RowAttributes string `json:"row_attributes" schema:"label=Row Attributes,type=text,placeholder=class=\"row\",help=Evaluated per item.,depends_on_key=operation,depends_on_values=convert_to_html_table"`

	CellAttributes string `json:"cell_attributes" schema:"label=Cell Attributes,type=text,placeholder=class=\"cell\",help=Evaluated per item.,depends_on_key=operation,depends_on_values=convert_to_html_table"`
}
