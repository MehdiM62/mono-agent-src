package data

// MarkdownNodeSchema documents the config keys MarkdownNode.Execute reads
// out of its map[string]interface{} config — see
// internal/nodes/control.SetNodeSchema's doc comment for why this is a
// companion struct rather than the runtime config, and
// internal/tools/schemagen for the tag grammar.
//
// Without a mode the node keeps its original behaviour (field → HTML in
// output_field). The modes follow n8n's Markdown node (markdown_n8n.go,
// markdown_to_html.go, markdown_from_html*.go); n8n's "options" collection
// is flattened into top-level keys (see N8nMarkdownParams).
type MarkdownNodeSchema struct {
	Mode string `json:"mode" schema:"label=Mode,type=select,options=markdown_to_html|html_to_markdown,help=Leave unset for the original behaviour: convert the Input Field from Markdown to HTML with GitHub-flavoured Markdown."`

	Markdown string `json:"markdown" schema:"label=Markdown,type=textarea,rows=6,placeholder={{ $json.body }},help=Markdown to convert; evaluated per item. Leave empty to read the Input Field.,depends_on_key=mode,depends_on_values=markdown_to_html"`

	HTML string `json:"html" schema:"label=HTML,type=textarea,rows=6,placeholder={{ $json.html }},help=HTML to convert; evaluated per item. Leave empty to read the Input Field.,depends_on_key=mode,depends_on_values=html_to_markdown"`

	Field string `json:"field" schema:"label=Input Field,type=text,placeholder=content,help=Item field holding the source text. Required without a mode; with a mode it is used when the Markdown/HTML value is empty."`

	DestinationKey string `json:"destination_key" schema:"label=Destination Key,type=text,placeholder=data,help=Dot path the result is written to (e.g. level1.level2.key). Defaults to the Output Field， then the Input Field， then data.,depends_on_key=mode,depends_on_values=markdown_to_html|html_to_markdown"`

	OutputField string `json:"output_field" schema:"label=Output Field,type=text,help=Defaults to the input field， overwriting it."`

	Unsafe bool `json:"unsafe" schema:"label=Allow Raw HTML,type=boolean,help=Pass raw HTML in the Markdown through. Default: off without a mode， on for markdown_to_html (like n8n)."`

	Tables bool `json:"tables" schema:"label=Tables Support,type=boolean,default=false,depends_on_key=mode,depends_on_values=markdown_to_html"`

	Strikethrough bool `json:"strikethrough" schema:"label=Strikethrough,type=boolean,default=false,help=~~text~~,depends_on_key=mode,depends_on_values=markdown_to_html"`

	Tasklists bool `json:"tasklists" schema:"label=GitHub Task Lists,type=boolean,default=false,depends_on_key=mode,depends_on_values=markdown_to_html"`

	SimpleLineBreaks bool `json:"simple_line_breaks" schema:"label=Simple Line Breaks,type=boolean,default=false,help=Render single line breaks as <br>.,depends_on_key=mode,depends_on_values=markdown_to_html"`

	SimplifiedAutoLink bool `json:"simplified_auto_link" schema:"label=Automatic Linking to URLs,type=boolean,default=false,depends_on_key=mode,depends_on_values=markdown_to_html"`

	NoHeaderID bool `json:"no_header_id" schema:"label=No Header ID,type=boolean,default=false,help=Do not add id attributes to headings.,depends_on_key=mode,depends_on_values=markdown_to_html"`

	CustomizedHeaderID bool `json:"customized_header_id" schema:"label=Customized Header ID,type=boolean,default=false,help=Use text in curly braces at the end of a heading as its id: # Title {my-id},depends_on_key=mode,depends_on_values=markdown_to_html"`

	GHCompatibleHeaderID bool `json:"gh_compatible_header_id" schema:"label=GitHub Compatible Header IDs,type=boolean,default=false,depends_on_key=mode,depends_on_values=markdown_to_html"`

	RawHeaderID bool `json:"raw_header_id" schema:"label=Raw Header ID,type=boolean,default=false,help=Only replace spaces and quotes with dashes in heading ids.,depends_on_key=mode,depends_on_values=markdown_to_html"`

	PrefixHeaderID string `json:"prefix_header_id" schema:"label=Prefix Header ID,type=text,placeholder=section,depends_on_key=mode,depends_on_values=markdown_to_html"`

	RawPrefixHeaderID bool `json:"raw_prefix_header_id" schema:"label=Raw Prefix Header ID,type=boolean,default=false,help=Add the prefix after the id is cleaned up， unchanged.,depends_on_key=mode,depends_on_values=markdown_to_html"`

	HeaderLevelStart int `json:"header_level_start" schema:"label=Header Level Start,type=number,default=1,min=1,max=6,depends_on_key=mode,depends_on_values=markdown_to_html"`

	OpenLinksInNewWindow bool `json:"open_links_in_new_window" schema:"label=Add Blank To Links,type=boolean,default=false,help=Add target=\"_blank\" to links.,depends_on_key=mode,depends_on_values=markdown_to_html"`

	CompleteHTMLDocument bool `json:"complete_html_document" schema:"label=Complete HTML Document,type=boolean,default=false,help=Wrap the result in <html><head><body>.,depends_on_key=mode,depends_on_values=markdown_to_html"`

	BulletMarker string `json:"bullet_marker" schema:"label=Bullet Marker,type=text,default=*,depends_on_key=mode,depends_on_values=html_to_markdown"`

	CodeBlockStyle string `json:"code_block_style" schema:"label=Style For Code Block,type=select,options=fence|indented,default=fence,depends_on_key=mode,depends_on_values=html_to_markdown"`

	CodeFence string "json:\"code_fence\" schema:\"label=Code Block Fence,type=text,default=```,depends_on_key=mode,depends_on_values=html_to_markdown\""

	EmDelimiter string `json:"em_delimiter" schema:"label=Emphasis Delimiter,type=text,default=_,depends_on_key=mode,depends_on_values=html_to_markdown"`

	StrongDelimiter string `json:"strong_delimiter" schema:"label=Strong Delimiter,type=text,default=**,depends_on_key=mode,depends_on_values=html_to_markdown"`

	HeadingStyle string `json:"heading_style" schema:"label=Heading Style,type=select,options=atx|setext,default=atx,help=setext underlines h1/h2; deeper headings stay atx.,depends_on_key=mode,depends_on_values=html_to_markdown"`

	Ignore string `json:"ignore" schema:"label=Ignored Elements,type=text,placeholder=nav， footer,help=Comma-separated elements dropped with their content.,depends_on_key=mode,depends_on_values=html_to_markdown"`

	BlockElements string `json:"block_elements" schema:"label=Treat As Blocks,type=text,placeholder=span， custom-tag,help=Comma-separated elements surrounded with blank lines.,depends_on_key=mode,depends_on_values=html_to_markdown"`

	KeepDataImages bool `json:"keep_data_images" schema:"label=Keep Images With Data,type=boolean,default=false,help=Keep images whose src is a data: URI.,depends_on_key=mode,depends_on_values=html_to_markdown"`

	MaxConsecutiveNewlines int `json:"max_consecutive_newlines" schema:"label=Max Consecutive New Lines,type=number,default=3,min=1,depends_on_key=mode,depends_on_values=html_to_markdown"`

	UseLinkReferenceDefinitions bool `json:"use_link_reference_definitions" schema:"label=Place URLs At The Bottom,type=boolean,default=false,help=Write links as [text][n] with the URLs listed at the end.,depends_on_key=mode,depends_on_values=html_to_markdown"`

	TextReplace string `json:"text_replace" schema:"label=Text Replacement Patterns (JSON),type=code,language=json,rows=3,placeholder=[{\"pattern\": \"foo\"， \"replacement\": \"bar\"}],help=Regex replacements applied to text content. Replacements use $1 / $& like JavaScript.,depends_on_key=mode,depends_on_values=html_to_markdown"`

	LineStartEscape string `json:"line_start_escape" schema:"label=Line Start Escape Pattern (JSON),type=code,language=json,rows=2,placeholder={\"pattern\": \"^#\"， \"replacement\": \"\\\\#\"},help=Overrides the built-in escaping of Markdown syntax at the start of a line.,depends_on_key=mode,depends_on_values=html_to_markdown"`

	GlobalEscape string `json:"global_escape" schema:"label=Global Escape Pattern (JSON),type=code,language=json,rows=2,placeholder={\"pattern\": \"[*_]\"， \"replacement\": \"\\\\$&\"},help=Overrides the built-in escaping of Markdown characters in text.,depends_on_key=mode,depends_on_values=html_to_markdown"`
}
