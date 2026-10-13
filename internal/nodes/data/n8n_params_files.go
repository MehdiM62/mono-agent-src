package data

// n8n parameter maps for the file nodes, used when importing n8n workflows.
//
// Each N8n<Node>Params map takes an n8n parameter name to the config key the
// mono-agent node reads. Parameters inside an n8n "collection" or
// "fixedCollection" are written as dotted paths from the node's parameters
// object ("options.fileName", "additionalFields.attendeesUi.attendeeValues");
// the importer reads the value at that path and stores it, unchanged, under
// the mapped flat key. Parameters with no entry have no mono-agent
// equivalent and are dropped.
//
// Each N8n<Node>Operations map takes an n8n operation value to ours where the
// two differ; values not listed are the same in both (csv, html, xlsx, …).
//
// n8n node type -> mono-agent node type:
//
//	n8n-nodes-base.convertToFile    -> data.convert_to_file
//	n8n-nodes-base.extractFromFile  -> data.extract_from_file
//	n8n-nodes-base.readWriteFile    -> data.read_write_file
//	n8n-nodes-base.iCal             -> data.ical
//	n8n-nodes-base.compression      -> data.compression

// icalN8nParams are the event fields shared by the iCalendar node and
// Convert to File's iCal operation.
var icalN8nParams = map[string]string{
	"title":  "title",
	"start":  "start",
	"end":    "end",
	"allDay": "all_day",
	"additionalFields.attendeesUi.attendeeValues":      "attendees",
	"additionalFields.busyStatus":                      "busy_status",
	"additionalFields.calName":                         "cal_name",
	"additionalFields.description":                     "description",
	"additionalFields.fileName":                        "file_name",
	"additionalFields.geolocationUi.geolocationValues": "geolocation",
	"additionalFields.location":                        "location",
	"additionalFields.recurrenceRule":                  "recurrence_rule",
	"additionalFields.organizerUi.organizerValues":     "organizer",
	"additionalFields.sequence":                        "sequence",
	"additionalFields.status":                          "status",
	"additionalFields.uid":                             "uid",
	"additionalFields.url":                             "url",
}

func withICal(m map[string]string) map[string]string {
	for k, v := range icalN8nParams {
		m[k] = v
	}
	return m
}

// N8nConvertToFileParams maps n8n Convert to File parameters.
var N8nConvertToFileParams = withICal(map[string]string{
	"operation":            "operation",
	"binaryPropertyName":   "binary_property_name",
	"sourceProperty":       "source_property",
	"mode":                 "mode",
	"options.addBOM":       "add_bom",
	"options.compression":  "compression",
	"options.dataIsBase64": "data_is_base64",
	"options.delimiter":    "delimiter",
	"options.encoding":     "encoding",
	"options.fileName":     "file_name",
	"options.format":       "format",
	"options.headerRow":    "header_row",
	"options.mimeType":     "mime_type",
	"options.sheetName":    "sheet_name",
})

// N8nConvertToFileOperations maps n8n Convert to File operation values.
// "xls" maps to itself and is refused at run time (use xlsx).
var N8nConvertToFileOperations = map[string]string{
	"iCal":     "ics",
	"toJson":   "json",
	"toText":   "text",
	"toBinary": "to_binary",
}

// N8nExtractFromFileParams maps n8n Extract from File parameters.
var N8nExtractFromFileParams = map[string]string{
	"operation":                 "operation",
	"binaryPropertyName":        "binary_property_name",
	"destinationKey":            "destination_key",
	"options.delimiter":         "delimiter",
	"options.enableBOM":         "strip_bom",
	"options.encoding":          "encoding",
	"options.fromLine":          "from_line",
	"options.headerRow":         "header_row",
	"options.includeEmptyCells": "include_empty_cells",
	"options.joinPages":         "join_pages",
	"options.keepSource":        "keep_source",
	"options.maxPages":          "max_pages",
	"options.maxRowCount":       "max_rows",
	"options.password":          "password",
	"options.range":             "range",
	"options.rawData":           "raw_data",
	"options.readAsString":      "read_as_string",
	"options.relaxQuotes":       "relax_quotes",
	"options.sheetName":         "sheet_name",
	"options.stripBOM":          "strip_bom",
}

// N8nExtractFromFileOperations maps n8n Extract from File operation values.
// "xls" maps to itself and is refused at run time (convert to xlsx).
var N8nExtractFromFileOperations = map[string]string{
	"fromIcs":         "ics",
	"fromJson":        "json",
	"binaryToPropery": "binary_to_property",
}

// N8nReadWriteFileParams maps n8n Read/Write Files from Disk parameters.
// The read option options.fileName and the write parameter fileName both
// land in file_name: read uses it to rename the files it returns, write as
// the path to write.
var N8nReadWriteFileParams = map[string]string{
	"operation":                "operation",
	"fileSelector":             "file_selector",
	"fileName":                 "file_name",
	"dataPropertyName":         "data_property_name",
	"options.append":           "append",
	"options.dataPropertyName": "data_property_name",
	"options.fileExtension":    "file_extension",
	"options.fileName":         "file_name",
	"options.literalBrackets":  "literal_brackets",
	"options.mimeType":         "mime_type",
}

// N8nICalendarParams maps n8n iCalendar (Create Event File) parameters. Its
// only operation, createEvent, is what data.ical does; the
// additionalFields.useWorkflowTimezone option has no equivalent (times
// carry their zone, or are read in the local zone).
var N8nICalendarParams = withICal(map[string]string{
	"binaryPropertyName": "binary_property_name",
})

// N8nCompressionParams maps n8n Compression parameters. n8n's operations
// compress and decompress keep their names.
var N8nCompressionParams = map[string]string{
	"operation":            "operation",
	"binaryPropertyName":   "binary_property_name",
	"outputFormat":         "output_format",
	"fileName":             "file_name",
	"binaryPropertyOutput": "binary_property_output",
	"outputPrefix":         "output_prefix",
}
