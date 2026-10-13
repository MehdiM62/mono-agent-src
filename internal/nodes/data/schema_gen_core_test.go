package data

import "github.com/monoes/mono-agent/internal/tools/schemagen"

func init() {
	for _, e := range []struct{ typ, file, name string }{
		{"data.split_out", "transform_schema.go", "SplitOutNodeSchema"},
		{"data.summarize", "transform_schema.go", "SummarizeNodeSchema"},
		{"data.rename_keys", "transform_schema.go", "RenameKeysNodeSchema"},
		{"data.jwt", "utility_schema.go", "JWTNodeSchema"},
		{"data.totp", "utility_schema.go", "TOTPNodeSchema"},
		{"data.debug_helper", "utility_schema.go", "DebugHelperNodeSchema"},
	} {
		newSchemas = append(newSchemas, schemagen.ManifestEntry{NodeType: e.typ, GoFile: "internal/nodes/data/" + e.file, StructName: e.name})
	}
}
