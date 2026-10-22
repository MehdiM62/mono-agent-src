// wails-app/app_history.go
package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monoes/mono-agent/internal/ai"
)

// The unified history and the access-aware chat bindings. Each shells out to
// `monoagentcli chat history …`; the CLI's JSON is the contract.

// HistoryFilter narrows ListHistory; every field is optional.
type HistoryFilter struct {
	Kind        string `json:"kind"` // chat or org
	Status      string `json:"status"`
	Application string `json:"application"`
	Query       string `json:"query"`
}

// HistoryRow is one conversation or one org run. The GUI computes unread.
type HistoryRow struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	Title           string   `json:"title"`
	Status          string   `json:"status"` // running, done, error, needs or idle
	UpdatedAt       string   `json:"updatedAt"`
	Runtime         string   `json:"runtime"`
	Model           string   `json:"model"`
	Effort          string   `json:"effort"`
	Access          string   `json:"access"`
	Mode            string   `json:"mode"`
	OrgMode         string   `json:"orgMode"`
	Cwd             string   `json:"cwd"`
	WorkflowContext string   `json:"workflowContext"`
	TaskID          string   `json:"taskId"`
	TaskStatus      string   `json:"taskStatus"`
	TaskGoal        string   `json:"taskGoal"`
	ApplicationID   string   `json:"applicationId"`
	OrgName         string   `json:"orgName"`
	RunID           string   `json:"runId"`
	CostUSD         *float64 `json:"costUsd"`
}

// HistoryPage is a page of rows; NextCursor is "" at the end. Warnings name
// the orgs whose runs could not be read.
type HistoryPage struct {
	Items      []HistoryRow `json:"items"`
	NextCursor string       `json:"next_cursor"`
	Warnings   []string     `json:"warnings"`
}

// ListHistory is `chat history list --all`: chats and org runs, newest first.
func (a *App) ListHistory(cursor string, limit int, filter HistoryFilter) (HistoryPage, error) {
	if err := a.signedOut(); err != nil {
		return HistoryPage{}, err
	}
	args := []string{"list", "--all", "--limit", fmt.Sprint(clampChatLimit(limit))}
	if cursor != "" {
		args = append(args, "--cursor", cursor)
	}
	for _, f := range [][2]string{{"--kind", filter.Kind}, {"--status", filter.Status}, {"--application", filter.Application}, {"--query", filter.Query}} {
		if f[1] != "" {
			args = append(args, f[0], f[1])
		}
	}
	var page HistoryPage
	if err := a.historyCLI(&page, args...); err != nil {
		return HistoryPage{}, err
	}
	if page.Items == nil {
		page.Items = []HistoryRow{}
	}
	if page.Warnings == nil {
		page.Warnings = []string{}
	}
	return page, nil
}

// Conversation is a chat conversation as the GUI keeps it.
type Conversation struct {
	ID              string `json:"id"`
	ProfileID       string `json:"profileId"`
	Backend         string `json:"backend"`
	WorkflowContext string `json:"workflowContext"`
	RuntimeID       string `json:"runtimeId"`
	Model           string `json:"model"`
	Effort          string `json:"effort"`
	Mode            string `json:"mode"`
	Access          string `json:"access"`
	Cwd             string `json:"cwd"`
	OrgMode         string `json:"orgMode"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

func conversationOf(r ai.ConversationRecord) Conversation {
	return Conversation{ID: r.ID, ProfileID: r.ProfileID, Backend: r.Backend, WorkflowContext: r.WorkflowContext,
		RuntimeID: r.RuntimeID, Model: r.Model, Effort: r.Effort, Mode: r.Mode, Access: r.Access, Cwd: r.Cwd,
		OrgMode: r.OrgMode, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

// CreateChatConversationAccess creates a conversation at an access level
// (scoped, coding, workspace or full; "" takes the coder default the operator
// set). Above scoped it works in cwd, or in the profile's folder when cwd is
// empty. The GUI window runs as the operator, so any level is allowed.
func (a *App) CreateChatConversationAccess(workflowID, runtimeID, model, effort, access, cwd string) (Conversation, error) {
	if err := a.signedOut(); err != nil {
		return Conversation{}, err
	}
	if workflowID == "" {
		workflowID = "general"
	}
	args := []string{"create", "--workflow", workflowID}
	if runtimeID != "" {
		args = append(args, "--runtime", runtimeID)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	if access != "" {
		args = append(args, "--access", access)
		if access != ai.AccessScoped {
			if cwd != "" {
				args = append(args, "--cwd", cwd)
			} else {
				args = append(args, "--profile-folder")
			}
		}
	}
	var rec ai.ConversationRecord
	if err := a.historyCLI(&rec, args...); err != nil {
		return Conversation{}, err
	}
	return conversationOf(rec), nil
}

// Survivor is a process a lowering could not stop.
type Survivor struct {
	Pid     int    `json:"pid"`
	Command string `json:"command"`
	Reason  string `json:"reason"`
}

// LoweredEffects is what lowering a conversation's access did.
type LoweredEffects struct {
	Stopped        []int      `json:"stopped"`
	Survivors      []Survivor `json:"survivors"`
	WorkersDropped bool       `json:"workersDropped"`
	TaskRevoked    bool       `json:"taskRevoked"`
}

// AccessChange is the result of SetChatAccess. Lowered is set for a lowering.
type AccessChange struct {
	Conversation   Conversation    `json:"conversation"`
	PreviousAccess string          `json:"previousAccess"`
	Lowered        *LoweredEffects `json:"lowered"`
}

// SetChatAccess is `chat history set-access`. Raising is the operator's and
// the GUI window is the operator; a scoped conversation raised gets the
// profile's folder.
func (a *App) SetChatAccess(id, level string) (AccessChange, error) {
	if err := a.signedOut(); err != nil {
		return AccessChange{}, err
	}
	var cur ai.ConversationRecord
	if err := a.historyCLI(&cur, "show", id); err != nil {
		return AccessChange{}, err
	}
	args := []string{"set-access", id, level}
	if cur.Cwd == "" && level != ai.AccessScoped {
		args = append(args, "--profile-folder")
	}
	var raw struct {
		ai.ConversationRecord
		Previous string `json:"previous_access"`
		Lowered  *struct {
			Stopped        []int      `json:"stopped"`
			Survivors      []Survivor `json:"survivors"`
			WorkersDropped bool       `json:"workers_dropped"`
			TaskRevoked    bool       `json:"task_revoked"`
		} `json:"lowered"`
	}
	if err := a.historyCLI(&raw, args...); err != nil {
		return AccessChange{}, err
	}
	out := AccessChange{Conversation: conversationOf(raw.ConversationRecord), PreviousAccess: raw.Previous}
	if l := raw.Lowered; l != nil {
		out.Lowered = &LoweredEffects{Stopped: l.Stopped, Survivors: l.Survivors, WorkersDropped: l.WorkersDropped, TaskRevoked: l.TaskRevoked}
		if out.Lowered.Stopped == nil {
			out.Lowered.Stopped = []int{}
		}
		if out.Lowered.Survivors == nil {
			out.Lowered.Survivors = []Survivor{}
		}
	}
	return out, nil
}

// DeleteChatResult says what DeleteChatConversations did, per id: Active ids
// have a turn running (stop it first), Failed ids carry the CLI's message.
type DeleteChatResult struct {
	Deleted []string          `json:"deleted"`
	Active  []string          `json:"active"`
	Failed  map[string]string `json:"failed"`
}

// DeleteChatConversations deletes each conversation with the single delete,
// so a turn that is running refuses exactly as DeleteChatConversation does.
func (a *App) DeleteChatConversations(ids []string) (DeleteChatResult, error) {
	res := DeleteChatResult{Deleted: []string{}, Active: []string{}, Failed: map[string]string{}}
	if err := a.signedOut(); err != nil {
		return res, err
	}
	for _, id := range ids {
		err := a.historyCLI(nil, "delete", id)
		var ae *aitaskError
		switch {
		case err == nil:
			res.Deleted = append(res.Deleted, id)
		case errors.As(err, &ae) && strings.Contains(ae.msg, "active turn"):
			res.Active = append(res.Active, id)
		default:
			res.Failed[id] = err.Error()
		}
	}
	return res, nil
}

// historyCLI runs `monoagentcli --profile P --json chat history <args>`.
func (a *App) historyCLI(result any, args ...string) error {
	full, err := a.scopedArgs(append([]string{"chat", "history"}, args...)...)
	if err != nil {
		return err
	}
	return a.jsonCLIInto(result, full)
}
