package workflow

import (
	"strings"
	"testing"
)

func TestSchemaProviderConsultedAfterFile(t *testing.T) {
	var gotFile []byte
	RegisterSchemaProvider(func(nodeType string, file []byte) ([]byte, bool) {
		switch nodeType {
		case "zz_test.provided":
			return []byte(`{"title":"Provided","credential_platform":null,"fields":[{"key":"a","label":"A","type":"text"}]}`), true
		case "core.if":
			gotFile = file
			return append([]byte(nil), file...), true
		}
		return nil, false
	})
	t.Cleanup(func() {
		schemaProvidersMu.Lock()
		schemaProviders = schemaProviders[:len(schemaProviders)-1]
		schemaProvidersMu.Unlock()
	})
	s, err := LoadDefaultSchema("zz_test.provided")
	if err != nil || len(s.Fields) != 1 || SchemaTitle("zz_test.provided") != "Provided" {
		t.Fatalf("provided schema: %+v %v", s, err)
	}
	if _, ok := ReadEmbeddedSchema("core.if"); !ok || !strings.Contains(string(gotFile), "condition") {
		t.Errorf("the provider must receive the schema file: %s", gotFile)
	}
	if _, ok := ReadEmbeddedSchema("zz_test.absent"); ok {
		t.Error("unknown types stay unknown")
	}
}
