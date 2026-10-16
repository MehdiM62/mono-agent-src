package browserjev

import (
	"os"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/testhome"
)

// Tests never touch the real ~/.monoagent (approval cards, kind tokens) or
// the OS keychain, and run as the user's own run unless they say otherwise:
// a `go test` started from a coding agent's shell inherits its markers,
// which would gate every run (gate.go).
func TestMain(m *testing.M) {
	for _, k := range append(browserctl.AgentContextEnv(), EnvAICaller) {
		os.Unsetenv(k)
	}
	guardSettle, approvalPoll = 0, 5*time.Millisecond
	keyring.MockInit()
	testhome.Main(m)
}
