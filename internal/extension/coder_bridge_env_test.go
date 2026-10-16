package extension_test

import (
	"slices"
	"testing"

	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/operator"
)

// The bridge strips from the side panel's coder turns exactly what makes a
// process not the operator's own terminal (operator.Marker): one list.
func TestCoderChildEnvMatchesTheOperatorMarkers(t *testing.T) {
	got, want := slices.Sorted(slices.Values(extension.CoderAgentEnv())), slices.Sorted(slices.Values(operator.MarkerEnv()))
	if !slices.Equal(got, want) {
		t.Fatalf("bridge strips %v\noperator.Marker reads %v", got, want)
	}
}
