package browserjev

import (
	"os"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/browserctl"
)

// Tests run as the user's own run unless they say otherwise: a `go test`
// started from a coding agent's shell inherits its markers, which would gate
// every run (gate.go).
func TestMain(m *testing.M) {
	for _, k := range append(browserctl.AgentContextEnv(), EnvAICaller) {
		os.Unsetenv(k)
	}
	guardSettle, approvalPoll = 0, 5*time.Millisecond
	os.Exit(m.Run())
}
