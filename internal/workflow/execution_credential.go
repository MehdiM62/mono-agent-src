package workflow

import (
	"strings"

	"github.com/monoes/mono-agent/internal/vault"
)

// injectCredential merges credential data into config if credential_id is
// present. GetOrResolve looks up the connections table by id or platform
// name and auto-refreshes expired OAuth tokens. It is scoped to the
// execution's profile so a workflow under profile B never resolves profile
// A's credentials.
func (r *runner) injectCredential(node WorkflowNode, config map[string]interface{}) {
	credIDRaw, ok := config["credential_id"]
	if !ok {
		return
	}
	credID, ok := credIDRaw.(string)
	if !ok || credID == "" {
		return
	}
	ctx := r.ctx
	injected := false

	if r.connStore != nil {
		conn, err := r.connStore.GetOrResolve(ctx, credID, vault.ProfileIDFromContext(ctx))
		if err != nil {
			r.logger.Warn().Err(err).
				Str("credential_id", credID).
				Msg("connections lookup failed")
			// conn is nil; injected stays false, miss handling below runs
		} else if conn != nil {
			// Merge all connection Data fields directly into config.
			for k, v := range conn.Data {
				config[k] = v
			}
			config["credential"] = conn.Data
			injected = true
		}
	}

	// For browser platform nodes (instagram, gemini, linkedin, etc.),
	// the credential_id is just the session username. Inject it as
	// "username" so BrowserNode can resolve the session.
	if !injected {
		browserPlatforms := map[string]bool{
			"instagram": true, "gemini": true, "linkedin": true,
			"x": true, "tiktok": true, "telegram": true,
		}
		parts := strings.SplitN(node.Type, ".", 2)
		if len(parts) == 2 && browserPlatforms[parts[0]] {
			config["username"] = credID
			injected = true
		}
	}

	if !injected {
		// Miss on every path. Include the execution's profile in
		// the log so cross-profile resolution failures are
		// diagnosable.
		r.logger.Warn().
			Str("credential_id", credID).
			Str("profile", vault.ProfileIDFromContext(ctx)).
			Msg("credential not resolved for profile")
	}
}
