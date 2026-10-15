// Package nodeprofile resolves which profile a workflow node runs for.
package nodeprofile

import (
	"context"
	"fmt"

	"github.com/monoes/mono-agent/internal/vault"
)

// Require returns the profile a node must act on: the execution's profile
// carried in ctx. A node's optional "profile_id" config must equal it (a
// workflow must not write into another profile). A ctx without a profile is
// an error -- there is no implicit "default".
func Require(ctx context.Context, node string, config map[string]interface{}) (string, error) {
	pid, ok := vault.ProfileIDInContext(ctx)
	if !ok {
		return "", fmt.Errorf("%s ran without a profile", node)
	}
	if want, _ := config["profile_id"].(string); want != "" && want != pid {
		return "", fmt.Errorf("%s: profile_id %q does not match the workflow's profile %q", node, want, pid)
	}
	return pid, nil
}
