package data

// ICalNodeSchema documents the config keys ICalNode.Execute reads — see
// internal/nodes/control.SetNodeSchema's doc comment for why this is a
// companion struct rather than the runtime config, and
// internal/tools/schemagen for the tag grammar. Every field except
// binary_property_name is resolved per item, so {{ $json.… }} expressions
// build one event per input item.
type ICalNodeSchema struct {
	Title string `json:"title" schema:"label=Event Title,type=text,placeholder={{ $json.title }}"`

	Start string `json:"start" schema:"label=Start,type=text,required,placeholder=2026-10-13T09:30:00Z,help=RFC 3339 date-time (a time without a zone is read in the local zone)， a date (2026-10-13)， or a Unix epoch."`

	End string `json:"end" schema:"label=End,type=text,placeholder=2026-10-13T10:30:00Z,help=Defaults to the start. For an all-day event this is the last day of the event."`

	AllDay bool `json:"all_day" schema:"label=All Day,type=boolean,default=false"`

	BinaryPropertyName string `json:"binary_property_name" schema:"label=Put Output File in Field,type=text,default=data,help=Binary property the .ics file is stored in."`

	Attendees string `json:"attendees" schema:"label=Attendees (JSON),type=code,language=json,rows=4,placeholder=[{\"name\": \"Ada\"， \"email\": \"ada@example.com\"， \"rsvp\": true}],help=List of {name， email， rsvp}."`

	BusyStatus string `json:"busy_status" schema:"label=Busy Status,type=select,options=|BUSY|TENTATIVE|FREE|OOF,help=Shown by Outlook (X-MICROSOFT-CDO-BUSYSTATUS)."`

	CalName string `json:"cal_name" schema:"label=Calendar Name,type=text,help=Name calendar apps give the imported calendar (X-WR-CALNAME)."`

	Description string `json:"description" schema:"label=Description,type=textarea"`

	FileName string `json:"file_name" schema:"label=File Name,type=text,default=event.ics"`

	Geolocation string `json:"geolocation" schema:"label=Geolocation (JSON),type=code,language=json,rows=2,placeholder={\"lat\": 52.52， \"lon\": 13.405}"`

	Location string `json:"location" schema:"label=Location,type=text"`

	RecurrenceRule string `json:"recurrence_rule" schema:"label=Recurrence Rule,type=text,placeholder=FREQ=WEEKLY;BYDAY=MO;COUNT=10,help=An RFC 5545 RRULE value."`

	Organizer string `json:"organizer" schema:"label=Organizer (JSON),type=code,language=json,rows=2,placeholder={\"name\": \"Ada\"， \"email\": \"ada@example.com\"}"`

	Sequence float64 `json:"sequence" schema:"label=Sequence,type=number,help=Revision number; raise it when sending an update of the same uid."`

	Status string `json:"status" schema:"label=Status,type=select,options=|CONFIRMED|CANCELLED|TENTATIVE"`

	UID string `json:"uid" schema:"label=UID,type=text,help=Stable id of the event; a random one is made when empty."`

	URL string `json:"url" schema:"label=URL,type=text"`
}
