package data

// ExtractFromFileNodeSchema documents the config keys
// ExtractFromFileNode.Execute reads — see internal/nodes/control.SetNodeSchema's
// doc comment for why this is a companion struct rather than the runtime
// config, and internal/tools/schemagen for the tag grammar.
//
// "xls" (Excel 97-2003) is not offered: Execute refuses it and asks for the
// file as .xlsx.
type ExtractFromFileNodeSchema struct {
	Operation string `json:"operation" schema:"label=Operation,type=select,required,options=csv|html|ics|json|ods|pdf|rtf|text|xml|xlsx|binary_to_property,default=csv,help=csv/html/ods/rtf/xlsx return one item per row; the others put the file's content in one field."`

	BinaryPropertyName string `json:"binary_property_name" schema:"label=Input Binary Field,type=text,required,default=data"`

	DestinationKey string `json:"destination_key" schema:"label=Destination Output Field,type=text,default=data,help=Field the content is written to (dot paths allowed).,depends_on_key=operation,depends_on_values=json|text|xml|ics|binary_to_property"`

	KeepSource string `json:"keep_source" schema:"label=Keep Source,type=select,options=json|binary|both,default=json,help=What of the input item to keep next to the result: its JSON， its files， or both.,depends_on_key=operation,depends_on_values=json|text|xml|ics|binary_to_property|pdf"`

	Encoding string `json:"encoding" schema:"label=Encoding,type=text,placeholder=utf8,help=utf8 (default; a UTF-16 byte order mark is honoured)， utf16le， utf16be， latin1， ascii， or a WHATWG label such as windows-1252.,depends_on_key=operation,depends_on_values=csv|json|text|xml|ics"`

	StripBOM bool `json:"strip_bom" schema:"label=Strip Byte Order Mark,type=boolean,default=true,depends_on_key=operation,depends_on_values=csv|json|text|xml|ics"`

	HeaderRow bool `json:"header_row" schema:"label=Header Row,type=boolean,default=true,help=The first row holds the column names. Off: each item is {row: [cells]}.,depends_on_key=operation,depends_on_values=csv|html|ods|rtf|xlsx"`

	IncludeEmptyCells bool `json:"include_empty_cells" schema:"label=Include Empty Cells,type=boolean,default=false,help=Keep empty cells as empty strings.,depends_on_key=operation,depends_on_values=csv|html|ods|rtf|xlsx"`

	Delimiter string `json:"delimiter" schema:"label=Delimiter,type=text,default=，,help=One character; \\t for a tab.,depends_on_key=operation,depends_on_values=csv"`

	FromLine float64 `json:"from_line" schema:"label=Starting Line,type=number,default=0,help=1-based line to start reading at; 0 or 1 read from the first line.,depends_on_key=operation,depends_on_values=csv"`

	MaxRows float64 `json:"max_rows" schema:"label=Max Rows,type=number,default=-1,help=Data rows to return; -1 for all.,depends_on_key=operation,depends_on_values=csv|html|ods|rtf|xlsx"`

	RelaxQuotes bool `json:"relax_quotes" schema:"label=Relax Quotes,type=boolean,default=false,help=Accept quotes inside unquoted fields instead of failing.,depends_on_key=operation,depends_on_values=csv"`

	SheetName string `json:"sheet_name" schema:"label=Sheet Name,type=text,help=The first sheet when empty.,depends_on_key=operation,depends_on_values=ods|xlsx"`

	Range string `json:"range" schema:"label=Range,type=text,placeholder=A1:D100,help=A 0-based row to start at (its row is the header)， or an A1 range.,depends_on_key=operation,depends_on_values=html|ods|xlsx"`

	RawData bool `json:"raw_data" schema:"label=Raw Data,type=boolean,default=false,help=Return every cell as its text; by default numeric and boolean cells become JSON numbers and booleans.,depends_on_key=operation,depends_on_values=ods|xlsx"`

	ReadAsString bool `json:"read_as_string" schema:"label=Read As String,type=boolean,default=false,help=Same as Raw Data: cells are returned as text.,depends_on_key=operation,depends_on_values=ods|xlsx"`

	JoinPages bool `json:"join_pages" schema:"label=Join Pages,type=boolean,default=true,help=Off: text is a list with one entry per page.,depends_on_key=operation,depends_on_values=pdf"`

	MaxPages float64 `json:"max_pages" schema:"label=Max Pages,type=number,default=0,help=Pages to read; 0 for all.,depends_on_key=operation,depends_on_values=pdf"`

	Password string `json:"password" schema:"label=Password,type=password,help=For encrypted PDFs.,depends_on_key=operation,depends_on_values=pdf"`
}
