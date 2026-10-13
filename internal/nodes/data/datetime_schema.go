package data

// DateTimeNodeSchema documents the config keys DateTimeNode.Execute reads
// out of its map[string]interface{} config — see
// internal/nodes/control.SetNodeSchema's doc comment for why this is a
// companion struct rather than the runtime config, and
// internal/tools/schemagen for the tag grammar.
//
// "output_field" has no entry in the hand-written schemas/data.datetime.json
// this replaces — Execute has always read it (defaulting to the same field,
// overwriting it) but the hand-written schema never exposed it, unlike every
// other node in this package that supports the same default-to-input-field
// convention (data.html, data.markdown, data.xml).
type DateTimeNodeSchema struct {
	Operation string `json:"operation" schema:"label=Operation,type=select,required,options=format|parse|add|subtract|diff|now|get_current_date|add_to_date|subtract_from_date|format_date|round_date|get_time_between_dates|extract_date,default=format,help=format…now work on a field with Go layouts. The other operations (n8n Date & Time v2) take a date value and write output_field_name."`

	Field string `json:"field" schema:"label=Date Field,type=text,placeholder=created_at,depends_on_key=operation,depends_on_values=format|parse|add|subtract|diff"`

	Field2 string `json:"field2" schema:"label=Second Date Field,type=text,placeholder=updated_at,depends_on_key=operation,depends_on_values=diff"`

	InputFormat string `json:"input_format" schema:"label=Input Format,type=text,placeholder=2006-01-02T15:04:05Z07:00,default=2006-01-02T15:04:05Z07:00,help=Go time layout string used to parse the input date field(s)."`

	OutputFormat string `json:"output_format" schema:"label=Output Format,type=text,placeholder=2006-01-02T15:04:05Z07:00,default=2006-01-02T15:04:05Z07:00,help=Go time layout string."`

	Duration string `json:"duration" schema:"label=Duration,type=text,placeholder=24h,help=add/subtract: a Go duration (24h， 90m). add_to_date/subtract_from_date: a number of time units.,depends_on_key=operation,depends_on_values=add|subtract|add_to_date|subtract_from_date"`

	OutputField string `json:"output_field" schema:"label=Output Field,type=text,help=Defaults to the date field， overwriting it."`

	Date string `json:"date" schema:"label=Date,type=text,placeholder={{ $json.created_at }},help=ISO date， Unix timestamp (s or ms) or common date text; evaluated per item.,depends_on_key=operation,depends_on_values=format_date|round_date|extract_date"`

	Magnitude string `json:"magnitude" schema:"label=Date To Change,type=text,placeholder={{ $json.created_at }},depends_on_key=operation,depends_on_values=add_to_date|subtract_from_date"`

	TimeUnit string `json:"time_unit" schema:"label=Time Unit,type=select,options=years|quarters|months|weeks|days|hours|minutes|seconds|milliseconds,default=days,depends_on_key=operation,depends_on_values=add_to_date|subtract_from_date"`

	Format string `json:"format" schema:"label=Format,type=select,options=MM/dd/yyyy|yyyy/MM/dd|MMMM dd yyyy|MM-dd-yyyy|yyyy-MM-dd|X|x|custom,default=MM/dd/yyyy,help=Luxon tokens (yyyy MM dd HH mm ss SSS ZZ EEEE …); X = Unix seconds， x = Unix ms.,depends_on_key=operation,depends_on_values=format_date"`

	CustomFormat string `json:"custom_format" schema:"label=Custom Format,type=text,placeholder=yyyy-MM-dd'T'HH:mm,depends_on_key=format,depends_on_values=custom"`

	FromFormat string `json:"from_format" schema:"label=From Format,type=text,placeholder=yyyyMMdd,help=Luxon format of the input date when it isn't ISO.,depends_on_key=operation,depends_on_values=format_date"`

	Mode string `json:"mode" schema:"label=Mode,type=select,options=round_down|round_up,default=round_down,depends_on_key=operation,depends_on_values=round_date"`

	ToNearest string `json:"to_nearest" schema:"label=To Nearest,type=select,options=year|month|week|day|hour|minute|second,default=month,depends_on_key=mode,depends_on_values=round_down"`

	To string `json:"to" schema:"label=To,type=select,options=year|month|week|day|hour|minute|second,default=month,help=Round up to the start of the next unit.,depends_on_key=mode,depends_on_values=round_up"`

	StartDate string `json:"start_date" schema:"label=Start Date,type=text,depends_on_key=operation,depends_on_values=get_time_between_dates"`

	EndDate string `json:"end_date" schema:"label=End Date,type=text,depends_on_key=operation,depends_on_values=get_time_between_dates"`

	Units string `json:"units" schema:"label=Units,type=text,default=day,placeholder=day， hour,help=Comma-separated: year， quarter， month， week， day， hour， minute， second， millisecond.,depends_on_key=operation,depends_on_values=get_time_between_dates"`

	ISOString bool `json:"iso_string" schema:"label=Output As ISO Duration,type=boolean,default=false,depends_on_key=operation,depends_on_values=get_time_between_dates"`

	Part string `json:"part" schema:"label=Part,type=select,options=year|month|week|day|hour|minute|second,default=month,depends_on_key=operation,depends_on_values=extract_date"`

	IncludeTime bool `json:"include_time" schema:"label=Include Current Time,type=boolean,default=true,depends_on_key=operation,depends_on_values=get_current_date"`

	Timezone string `json:"timezone" schema:"label=Timezone,type=text,placeholder=Europe/Berlin,help=IANA zone for reading dates without an offset and for the output. Default: this machine's zone.,depends_on_key=operation,depends_on_values=get_current_date|add_to_date|subtract_from_date|format_date|round_date|get_time_between_dates|extract_date"`

	OutputFieldName string `json:"output_field_name" schema:"label=Output Field Name,type=text,help=Defaults to n8n's name for the operation (currentDate， newDate， formattedDate， roundedDate， timeDifference， datePart).,depends_on_key=operation,depends_on_values=get_current_date|add_to_date|subtract_from_date|format_date|round_date|get_time_between_dates|extract_date"`

	IncludeInputFields bool `json:"include_input_fields" schema:"label=Include Input Fields,type=boolean,default=false,depends_on_key=operation,depends_on_values=get_current_date|add_to_date|subtract_from_date|format_date|round_date|get_time_between_dates|extract_date"`
}
