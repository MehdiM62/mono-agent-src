package workflow

import "sync"

// SchemaProvider supplies or rewrites a node type's schema JSON at run time.
// fileSchema is the node type's schema file (nil when there is none). It
// returns the schema to use and true, or false to leave the resolution
// unchanged. API packages (internal/apipkg) use it to describe their app.*
// node types and to append their operations to the native node they extend.
type SchemaProvider func(nodeType string, fileSchema []byte) ([]byte, bool)

var (
	schemaProvidersMu sync.RWMutex
	schemaProviders   []SchemaProvider
)

// RegisterSchemaProvider adds p to the providers consulted by
// LoadDefaultSchema / ReadEmbeddedSchema, after the schema file and before
// generated action schemas.
func RegisterSchemaProvider(p SchemaProvider) {
	schemaProvidersMu.Lock()
	schemaProviders = append(schemaProviders, p)
	schemaProvidersMu.Unlock()
}

// providedSchema runs the providers in order; the first that answers wins.
func providedSchema(nodeType string, fileSchema []byte) ([]byte, bool) {
	schemaProvidersMu.RLock()
	ps := append([]SchemaProvider(nil), schemaProviders...)
	schemaProvidersMu.RUnlock()
	for _, p := range ps {
		if data, ok := p(nodeType, fileSchema); ok {
			return data, true
		}
	}
	return nil, false
}

// ReadSchemaFile returns a node type's own schema file, without any
// provider (API package) merged in.
func ReadSchemaFile(nodeType string) ([]byte, bool) {
	return schemaFile(nodeType)
}
