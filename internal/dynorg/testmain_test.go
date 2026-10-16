package dynorg

import (
	"testing"

	"github.com/monoes/mono-agent/internal/testhome"
)

// Tests never touch the real ~/.monoagent: workers' kind tokens go to
// ~/.monoagent/agent-kinds.
func TestMain(m *testing.M) { testhome.Main(m) }
