// Package nodeprofile resolves which profile a workflow node runs for.
package nodeprofile

import (
	"context"
	"fmt"

	"github.com/monoes/mono-agent/internal/logging"
	"github.com/monoes/mono-agent/internal/vault"
)

// legacyDefault is the profile_id older schemas wrote into saved workflows.
const legacyDefault = "default"

// Require returns the profile a node must act on: the execution's profile
// carried in ctx. A node's optional "profile_id" config must equal it (a
// workflow must not write into another profile). A ctx without a profile is
// an error -- there is no implicit "default".
//
// The old schema default saved profile_id "default" into workflows; that
// literal, when it differs from the run's profile, is ignored with a warning
// rather than failing the run. Any other differing value is an error.
func Require(ctx context.Context, node string, config map[string]interface{}) (string, error) {
	pid, ok := vault.ProfileIDInContext(ctx)
	if !ok {
		return "", fmt.Errorf("%s ran without a profile", node)
	}
	if want, _ := config["profile_id"].(string); want != "" && want != pid {
		if want == legacyDefault {
			logging.FromContext(ctx).Warn(fmt.Sprintf("node %s: ignoring legacy profile_id=default; using the workflow's profile %s", node, pid))
			return pid, nil
		}
		return "", fmt.Errorf("%s: profile_id %q does not match the workflow's profile %q", node, want, pid)
	}
	return pid, nil
}
