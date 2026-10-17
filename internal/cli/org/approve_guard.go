package org

import (
	"context"
	"strings"

	"github.com/monoes/mono-agent/internal/cli/orgenv"
	"github.com/monoes/mono-agent/internal/operator"
	"github.com/monoes/mono-agent/internal/orgdecide"
)

// refuseSendingApproval refuses approving an approval of org for action
// when that lets a message to a person go out (a call of a granted
// automation whose workflow may send: orgdecide.ItemSends) and this process
// is not the operator's own terminal (internal/operator: an agent, the chat,
// a workflow). Only the user approves a message to a person; denying it
// stays anyone's. Approvals of other actions pass. When whether it sends
// can't be told, it is refused.
func (env *orgEnv) refuseSendingApproval(ctx context.Context, org, action string) error {
	why := operator.Refusal()
	if why == "" {
		return nil
	}
	it := orgdecide.Item{Kind: orgdecide.KindApproval, Class: orgdecide.ClassForAction(action), Action: action}
	if !strings.HasPrefix(it.Class, "grant:") {
		return nil
	}
	db, profileID, _, err := env.Profile()
	if err != nil {
		return env.d.InvalidInput("approving %s may let a message to a person go out, and that can't be checked (%v): %s; deny it, or approve it yourself from a terminal or the app", action, err, why)
	}
	if orgdecide.ItemSends(ctx, db.DB, profileID, org, it, orgenv.GrantWorkflows(db, env.OpenWorkflowStore)) {
		return env.d.InvalidInput("approving %s lets a message to a person go out, which only the user approves: %s; deny it, or approve it yourself from a terminal or the app", action, why)
	}
	return nil
}
