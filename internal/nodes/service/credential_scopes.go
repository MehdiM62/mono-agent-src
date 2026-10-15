package service

import "github.com/monoes/mono-agent/internal/connections"

// credentialScopeCheck fails early, naming the exact `monoagentcli connect`
// command to run, when the node's credential is an umbrella connection
// (google, microsoft) that was not granted the scopes product needs.
// Per-product connections (gmail, google_sheets, outlook, …) always pass.
func credentialScopeCheck(config map[string]interface{}, product string) error {
	cred, _ := config["credential"].(map[string]interface{})
	return connections.CheckCredentialScopes(cred, product)
}
