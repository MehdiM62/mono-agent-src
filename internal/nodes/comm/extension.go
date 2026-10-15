package comm

import (
	"strings"

	"github.com/monoes/mono-agent/internal/apipkg"
)

// A declarative API package can extend a native node with more operations
// ("extends": "comm.x", internal/apipkg FORMAT.md §11). Every native Execute
// first asks extensionOwns and, when it says yes, hands the whole config to
// apipkg.ExecuteExtension before any native validation runs.
//
// Native nodes have no "resource" field, so a config with a resource is
// the package's. Without a resource, an operation goes to the package only
// when the package defines it without a resource; such an operation must
// not reuse a native operation's name or alias, or it would take that
// operation over (the schema merge already refuses to list the duplicate).

// extensionOwns reports whether the package extending nodeType handles
// config's (resource, operation).
func extensionOwns(nodeType string, config map[string]interface{}) bool {
	op := strings.TrimSpace(str(config["operation"]))
	if op == "" {
		return false
	}
	return apipkg.HasExtension(nodeType, strings.TrimSpace(str(config["resource"])), op)
}

// str reads a string config value ("" for anything else).
func str(v interface{}) string {
	s, _ := v.(string)
	return s
}
