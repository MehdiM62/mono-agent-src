package operator

import "strings"

// reservedConfigs are the configs-table rows (`config get|set|delete`) that
// gate approvals, permissions or sandboxing. Only the operator's own terminal
// writes them: `config set` and `config delete` refuse anything else
// (internal/cli/configcli), as the commands made for them do.
//
//   - browser_agent_permissions: each kind of agent's level in the user's
//     browser and the site rules (browserctl.PermissionsSetting; `browser
//     permissions set|site`).
//   - browser_agent_allowlist: the older per-site allow rules, still read as
//     allow rules (browserctl.AllowlistSetting; `browser allow`).
//   - coder_mode: coder mode on or off, and sendApproval: whether an agent's
//     sends wait for approval (internal/cli/chat, internal/hilsend; `coder
//     enable`, `coder set --send-approval`).
//   - sandbox.fallback: what an agent turn does when its runtime has no
//     sandbox for it (`sandbox fallback --set`).
//   - automation.autoRepair: the recorder applies selector repairs without
//     asking (automation.AutoRepairConfig).
//   - coder_projects: the folder a site's queued coder chats work in, with
//     full access (`coder projects map|unmap`).
//
// Not reserved: memory.runs (the run-memory opt-out), workflow.error_workflow
// (the default error workflow, which any workflow can also name for itself)
// and the social actions' selector configs. The task board's agent
// access, the send always-allow rules and org grants live in other tables,
// which `config` does not reach.
var reservedConfigs = []string{
	"browser_agent_permissions",
	"browser_agent_allowlist",
	"coder_mode",
	"sandbox.fallback",
	"automation.autoRepair",
	"coder_projects",
}

// ReservedConfig reports whether name is a reserved config row, in any case
// and with spaces around it.
func ReservedConfig(name string) bool {
	name = strings.TrimSpace(name)
	for _, r := range reservedConfigs {
		if strings.EqualFold(name, r) {
			return true
		}
	}
	return false
}
