package extension_test

import (
	"slices"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/extension"
)

// The bridge strips from its coder children exactly what the `browser`
// commands read to decide that an agent is asking.
func TestCoderChildEnvMatchesTheBrowserDetector(t *testing.T) {
	got, want := slices.Sorted(slices.Values(extension.CoderAgentEnv())), slices.Sorted(slices.Values(browserctl.AgentContextEnv()))
	if !slices.Equal(got, want) {
		t.Fatalf("bridge strips %v\nbrowserctl reads %v", got, want)
	}
}
