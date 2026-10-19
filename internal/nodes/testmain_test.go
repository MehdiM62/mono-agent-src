package nodes

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/operator"
	"github.com/monoes/mono-agent/internal/testhome"
)

// Tests never touch the real ~/.monoagent or the OS keychain, and run as
// the user's own process: a test that runs as an agent sets a marker itself
// (an agent's browser runs are gated, agent_run_gate.go).
func TestMain(m *testing.M) {
	for _, k := range operator.MarkerEnv() {
		os.Unsetenv(k)
	}
	keyring.MockInit()
	testhome.Main(m)
}
