package doctor

import (
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/health"
	"github.com/monoes/mono-agent/internal/testhome"
)

// The commands' report, fix loop and setup steps are tested here against registries of fake
// checks and fixes; the real checks over the real machine (newHealthEnv) are tested in
// cmd/monoagentcli, through the CLI.

// Tests never touch the real ~/.monoagent or the OS keychain.
func TestMain(m *testing.M) {
	keyring.MockInit()
	testhome.Main(m)
}

// testDeps is the CLI around the commands: an environment with the paths only.
func testDeps(jsonOut bool) *Deps {
	return &Deps{
		JSONOutput: func() bool { return jsonOut },
		NewEnv: func([]string) (*health.Env, func()) {
			home := filepath.Join("/nonexistent", "home")
			return &health.Env{Home: home, DataDir: filepath.Join(home, ".monoagent"), DBPath: filepath.Join(home, ".monoagent", "x.db")}, func() {}
		},
		FinishStreamed: func(io.Writer, bool, error, string) error { return errors.New("not used") },
	}
}
