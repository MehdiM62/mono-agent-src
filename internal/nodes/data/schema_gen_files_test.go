package data

import "github.com/monoes/mono-agent/internal/tools/schemagen"

func init() {
	newSchemas = append(newSchemas,
		schemagen.ManifestEntry{NodeType: "data.convert_to_file", GoFile: "internal/nodes/data/convert_to_file_schema.go", StructName: "ConvertToFileNodeSchema"},
		schemagen.ManifestEntry{NodeType: "data.extract_from_file", GoFile: "internal/nodes/data/extract_from_file_schema.go", StructName: "ExtractFromFileNodeSchema"},
		schemagen.ManifestEntry{NodeType: "data.read_write_file", GoFile: "internal/nodes/data/read_write_file_schema.go", StructName: "ReadWriteFileNodeSchema"},
		schemagen.ManifestEntry{NodeType: "data.ical", GoFile: "internal/nodes/data/ical_schema.go", StructName: "ICalNodeSchema"},
	)
}
