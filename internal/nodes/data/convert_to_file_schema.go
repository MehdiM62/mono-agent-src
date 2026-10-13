package data

// ConvertToFileNodeSchema documents the config keys ConvertToFileNode.Execute
// reads — see internal/nodes/control.SetNodeSchema's doc comment for why
// this is a companion struct rather than the runtime config, and
// internal/tools/schemagen for the tag grammar.
//
// Operation "ics" reads the same event fields as data.ical (ical_schema.go);
// they are repeated here, shown only for that operation. "xls" (Excel
// 97-2003) is not offered: Execute refuses it and points at xlsx.
type ConvertToFileNodeSchema struct {
	Operation string `json:"operation" schema:"label=Operation,type=select,required,options=csv|html|ics|json|ods|rtf|text|xlsx|to_binary,default=csv,help=csv/html/ods/rtf/xlsx and json (mode once) write all items to one file; text， to_binary， ics and json (mode each) write one file per item."`

	BinaryPropertyName string `json:"binary_property_name" schema:"label=Put Output File in Field,type=text,required,default=data"`

	SourceProperty string `json:"source_property" schema:"label=Source Field,type=text,placeholder=content,help=Field holding the text (text) or the base64 string (to_binary). Dot paths reach nested fields.,depends_on_key=operation,depends_on_values=text|to_binary"`

	Mode string `json:"mode" schema:"label=Mode,type=select,options=once|each,default=once,help=once: all items to one JSON array file. each: one file per item.,depends_on_key=operation,depends_on_values=json"`

	Format bool `json:"format" schema:"label=Pretty-print JSON,type=boolean,default=false,depends_on_key=operation,depends_on_values=json"`

	HeaderRow bool `json:"header_row" schema:"label=Header Row,type=boolean,default=true,help=Write the column names as the first row.,depends_on_key=operation,depends_on_values=csv|html|ods|rtf|xlsx"`

	Delimiter string `json:"delimiter" schema:"label=Delimiter,type=text,default=，,help=One character; \\t for a tab.,depends_on_key=operation,depends_on_values=csv"`

	SheetName string `json:"sheet_name" schema:"label=Sheet Name,type=text,default=Sheet,depends_on_key=operation,depends_on_values=ods|xlsx"`

	Compression bool `json:"compression" schema:"label=Compress,type=boolean,default=false,help=Deflate the parts of the file (smaller file).,depends_on_key=operation,depends_on_values=ods|xlsx"`

	Encoding string `json:"encoding" schema:"label=Encoding,type=text,default=utf8,placeholder=utf8,help=utf8， utf16le (ucs2)， utf16be， latin1， ascii， or any WHATWG label such as windows-1252.,depends_on_key=operation,depends_on_values=json|text|to_binary"`

	AddBOM bool `json:"add_bom" schema:"label=Add Byte Order Mark,type=boolean,default=false,help=Only for UTF-8 and UTF-16.,depends_on_key=operation,depends_on_values=json|text|to_binary"`

	DataIsBase64 bool `json:"data_is_base64" schema:"label=Data Is Base64,type=boolean,default=true,help=Off: the field holds text， written in the chosen encoding.,depends_on_key=operation,depends_on_values=to_binary"`

	MimeType string `json:"mime_type" schema:"label=MIME Type,type=text,placeholder=application/pdf,help=Detected from the content when empty.,depends_on_key=operation,depends_on_values=to_binary|text"`

	FileName string `json:"file_name" schema:"label=File Name,type=text,placeholder=report.csv,help=Defaults to File.<ext> for tables， file.json / file.txt， event.ics."`

	Title string `json:"title" schema:"label=Event Title,type=text,depends_on_key=operation,depends_on_values=ics"`

	Start string `json:"start" schema:"label=Start,type=text,placeholder=2026-10-13T09:30:00Z,help=RFC 3339 date-time， a date， or a Unix epoch.,depends_on_key=operation,depends_on_values=ics"`

	End string `json:"end" schema:"label=End,type=text,depends_on_key=operation,depends_on_values=ics"`

	AllDay bool `json:"all_day" schema:"label=All Day,type=boolean,default=false,depends_on_key=operation,depends_on_values=ics"`

	Attendees string `json:"attendees" schema:"label=Attendees (JSON),type=code,language=json,rows=3,help=List of {name， email， rsvp}.,depends_on_key=operation,depends_on_values=ics"`

	BusyStatus string `json:"busy_status" schema:"label=Busy Status,type=select,options=|BUSY|TENTATIVE|FREE|OOF,depends_on_key=operation,depends_on_values=ics"`

	CalName string `json:"cal_name" schema:"label=Calendar Name,type=text,depends_on_key=operation,depends_on_values=ics"`

	Description string `json:"description" schema:"label=Description,type=textarea,depends_on_key=operation,depends_on_values=ics"`

	Geolocation string `json:"geolocation" schema:"label=Geolocation (JSON),type=code,language=json,rows=2,placeholder={\"lat\": 52.52， \"lon\": 13.405},depends_on_key=operation,depends_on_values=ics"`

	Location string `json:"location" schema:"label=Location,type=text,depends_on_key=operation,depends_on_values=ics"`

	RecurrenceRule string `json:"recurrence_rule" schema:"label=Recurrence Rule,type=text,placeholder=FREQ=WEEKLY;COUNT=10,depends_on_key=operation,depends_on_values=ics"`

	Organizer string `json:"organizer" schema:"label=Organizer (JSON),type=code,language=json,rows=2,placeholder={\"name\": \"Ada\"， \"email\": \"ada@example.com\"},depends_on_key=operation,depends_on_values=ics"`

	Sequence float64 `json:"sequence" schema:"label=Sequence,type=number,depends_on_key=operation,depends_on_values=ics"`

	Status string `json:"status" schema:"label=Status,type=select,options=|CONFIRMED|CANCELLED|TENTATIVE,depends_on_key=operation,depends_on_values=ics"`

	UID string `json:"uid" schema:"label=UID,type=text,depends_on_key=operation,depends_on_values=ics"`

	URL string `json:"url" schema:"label=URL,type=text,depends_on_key=operation,depends_on_values=ics"`
}
