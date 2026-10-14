package daemon

import (
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/testhome"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	testhome.Main(m)
}
