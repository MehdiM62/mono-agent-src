package operator

import (
	"testing"

	"github.com/monoes/mono-agent/internal/automation"
	"github.com/monoes/mono-agent/internal/browserctl"
)

// The rows that gate approvals, permissions or sandboxing are reserved, by
// the names their readers use; other rows are not.
func TestReservedConfig(t *testing.T) {
	for _, name := range []string{browserctl.PermissionsSetting, browserctl.AllowlistSetting, "coder_mode", "sandbox.fallback",
		automation.AutoRepairConfig, "coder_projects", " Coder_Mode ", "BROWSER_AGENT_PERMISSIONS"} {
		if !ReservedConfig(name) {
			t.Errorf("%q is not reserved", name)
		}
	}
	for _, name := range []string{"memory.runs", "workflow.error_workflow", "api_key", "coder_mode.x", ""} {
		if ReservedConfig(name) {
			t.Errorf("%q is reserved", name)
		}
	}
}
