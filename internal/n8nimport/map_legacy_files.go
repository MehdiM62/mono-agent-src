package n8nimport

// n8n's legacy file nodes (Read Binary File(s), Read PDF, Move Binary
// Data), replaced in n8n by Read/Write Files, Extract from File and
// Convert to File, map onto the mono nodes of those replacements.

func init() {
	Register("n8n-nodes-base.readBinaryFile", mapReadBinaryFile)
	Register("n8n-nodes-base.readBinaryFiles", mapReadBinaryFiles)
	Register("n8n-nodes-base.readPDF", mapReadPDF)
	Register("n8n-nodes-base.moveBinaryData", mapMoveBinaryData)
}

func mapReadBinaryFile(c *MapContext) Mapping {
	c.Warn("filePath", c.Raw("filePath"), "the item's JSON becomes the file's details (n8n kept the input JSON next to the file)")
	return Mapping{Type: "data.read_write_file", Config: map[string]interface{}{"operation": "read",
		"file_selector": c.Str("filePath", ""), "data_property_name": c.Str("dataPropertyName", "data")}}
}

func mapReadBinaryFiles(c *MapContext) Mapping {
	return Mapping{Type: "data.read_write_file", Config: map[string]interface{}{"operation": "read",
		"file_selector": c.Str("fileSelector", ""), "data_property_name": c.Str("dataPropertyName", "data")}}
}

// mapReadPDF: the PDF's text and details replace the item's JSON; its
// files stay, as in n8n.
func mapReadPDF(c *MapContext) Mapping {
	cfg := map[string]interface{}{"operation": "pdf", "binary_property_name": c.Str("binaryPropertyName", "data"),
		"keep_source": "binary"}
	if c.Bool("encrypted", false) {
		cfg["password"] = c.Str("password", "")
	} else {
		c.Ignore("password")
	}
	return Mapping{Type: "data.extract_from_file", Config: cfg}
}

func mapMoveBinaryData(c *MapContext) Mapping {
	if c.Literal("mode", "binaryToJson") == "jsonToBinary" {
		return moveJSONToBinary(c)
	}
	keep := c.Bool("options.keepSource", false)
	c.Ignore("options.useRawData", "options.dataIsBase64", "options.addBOM", "options.fileName", "options.mimeType")
	cfg := map[string]interface{}{"operation": "text", "binary_property_name": c.Str("sourceKey", "data"),
		"destination_key": c.Str("destinationKey", "data"), "keep_source": map[bool]string{true: "both", false: "json"}[keep]}
	if e := c.Str("options.encoding", ""); e != "" {
		cfg["encoding"] = e
	}
	cfg["strip_bom"] = c.Bool("options.stripBOM", true)
	switch {
	case c.Bool("setAllData", true):
		cfg["operation"], cfg["destination_key"] = "json", "data"
		c.Warn("setAllData", true, "the file's JSON is written to the field \"data\"; n8n replaced the whole item with it")
	case c.Bool("options.keepAsBase64", false):
		cfg["operation"] = "binary_to_property"
	case c.Bool("options.jsonParse", false):
		cfg["operation"] = "json"
	}
	return Mapping{Type: "data.extract_from_file", Config: cfg}
}

func moveJSONToBinary(c *MapContext) Mapping {
	c.Ignore("options.stripBOM", "options.jsonParse", "options.keepAsBase64")
	cfg := map[string]interface{}{"binary_property_name": c.Str("destinationKey", "data")}
	for n8n, ours := range map[string]string{"options.fileName": "file_name", "options.mimeType": "mime_type", "options.encoding": "encoding"} {
		if v := c.Str(n8n, ""); v != "" {
			cfg[ours] = v
		}
	}
	if c.Bool("options.addBOM", false) {
		cfg["add_bom"] = true
	}
	switch {
	case c.Bool("convertAllData", true):
		cfg["operation"], cfg["mode"] = "json", "each"
		c.Ignore("sourceKey", "options.useRawData", "options.dataIsBase64")
	case c.Bool("options.dataIsBase64", false):
		cfg["operation"], cfg["source_property"] = "to_binary", c.Str("sourceKey", "data")
		c.Ignore("options.useRawData")
	default:
		cfg["operation"], cfg["source_property"] = "text", c.Str("sourceKey", "data")
		if !c.Bool("options.useRawData", false) {
			c.Warn("options.useRawData", false, "the field is written as text; n8n wrote its JSON encoding (strings in quotes)")
		}
	}
	return Mapping{Type: "data.convert_to_file", Config: cfg}
}
