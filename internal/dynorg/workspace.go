package dynorg

import (
	"context"
	"strings"
)

// ProfileContext builds the active profile's context block (name, folder,
// connected accounts, counts and recent items of tasks, documents, people,
// workflows, orgs, vault item names): internal/profilectx. The caller wires
// it once at startup; nil skips the block. It is included in the lead's and
// every worker's system prompt and must never contain secret values.
var ProfileContext func(ctx context.Context) string

// profileBlock is the profile context as a prompt section, or "".
func profileBlock(ctx context.Context) string {
	if ProfileContext == nil {
		return ""
	}
	t := strings.TrimSpace(ProfileContext(ctx))
	if t == "" {
		return ""
	}
	return "\n\n## Profile context\n\n" + clipText(t, maxProfileContext) + "\n"
}

const maxProfileContext = 6000

// workspaceRules are the surface rules of the workspace access profile,
// shared by the worker prompt and the lead's.
const workspaceRules = "You have full access to this folder and the web, and to every part of the user's mono-agent through " +
	"`monoagentcli` on your PATH (`monoagentcli ref commands` lists what it can do). Use the surface that owns the work: " +
	"an app being run (job applications, social, browser) -> work on that app's tab with `browser` (or `node run` for its nodes); " +
	"to-dos -> `task`; files and documents -> `profile documents`; contacts -> `people`; " +
	"credentials -> the vault (use item names and paths, never print or write a value); " +
	"repeatable work -> build or reuse a WORKFLOW first (`workflow list`, `workflow validate`, `workflow run`) and only then bring in org roles. " +
	"Other writes (tasks, documents, people records, workflows) need no approval."

// sendRule replaces the no-messages rule for the workspace profile:
// preparing is free, finalizing a send never is.
const sendRule = "Prepare freely, but NEVER finalize a send. Everything outbound (email, DM, comment, post, publish, connection request) " +
	"must go through the send-approval queue: queue it with `monoagentcli hil ...` or the send nodes, which create a pending approval " +
	"the user answers. Do not try to bypass it (no direct API calls, scripts or browser clicks that send), and do not read the user's " +
	"private messages unless the brief asks you to."
