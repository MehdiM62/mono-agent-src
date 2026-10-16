package extension_test

import (
	"slices"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/operator"
)

func sameSet(t *testing.T, what string, got, want []string) {
	t.Helper()
	got, want = slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(want))
	if !slices.Equal(got, want) {
		t.Fatalf("%s: bridge strips %v\nsource reads %v", what, got, want)
	}
}

// What the coder.* commands run without is exactly the agent markers the
// `browser` commands and `coder queue` read (as before #831).
func TestCoderChildEnvMatchesTheBrowserDetector(t *testing.T) {
	sameSet(t, "coder.* commands", extension.CoderAgentEnv(), browserctl.AgentContextEnv())
}

// What the side panel's coder turns run without is exactly what makes a
// process not the operator's own terminal (operator.Marker): one list.
func TestCoderTurnEnvMatchesTheOperatorMarkers(t *testing.T) {
	sameSet(t, "coder turns", extension.CoderTurnEnv(), operator.MarkerEnv())
}
