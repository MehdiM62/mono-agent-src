package workflow

import (
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/testhome"
)

// Tests never touch the real ~/.monoagent (the trace signing key) or the OS
// keychain.
func TestMain(m *testing.M) {
	keyring.MockInit()
	testhome.Main(m)
}
