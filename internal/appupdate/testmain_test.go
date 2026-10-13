package appupdate

import (
	"testing"

	"github.com/monoes/mono-agent/internal/testhome"
)

// A throwaway HOME (release-state.json never lands in the real one) and no
// network beyond loopback.
func TestMain(m *testing.M) { testhome.Main(m) }
