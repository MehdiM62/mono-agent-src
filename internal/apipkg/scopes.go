package apipkg

import "github.com/monoes/mono-agent/internal/connections"

// Package credentials with a scope_group are checked against the umbrella
// google/microsoft connection's granted scopes.
func init() { ScopeCheck = connections.CheckCredentialScopes }
