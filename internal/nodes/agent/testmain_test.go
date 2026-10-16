package agent

import (
	"testing"

	"github.com/monoes/mono-agent/internal/testhome"
)

// Tests never touch the real ~/.monoagent: agent turns write their prompt
// files to ~/.monoagent/tmp.
func TestMain(m *testing.M) { testhome.Main(m) }
