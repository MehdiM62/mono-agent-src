package chat

import (
	"context"
	"database/sql"

	"github.com/monoes/mono-agent/internal/dynorg"
	"github.com/monoes/mono-agent/internal/tasks"
)

// missionActor is the name the coder lead plans missions under.
const missionActor = "mission-lead"

// missionConfig is the lead's mission setup for this turn: the planning tools
// and prompt need coding access or more and a confirmed mission (the user's
// one click, D2), which a conversation with an open mission session keeps
// across turns and restarts. Below coding access it is nil.
func missionConfig(ctx context.Context, db *sql.DB, profileID, conversationID string, t coderTurn) *dynorg.MissionConfig {
	if !dynorg.MissionAccess(t.access) || conversationID == "" {
		return nil
	}
	m := &dynorg.MissionConfig{
		Store: tasks.NewStore(db), ProfileID: profileID, ConversationID: conversationID,
		Actor: missionActor, Access: t.access, Confirmed: t.missionConfirmed,
	}
	if !m.Confirmed && m.HasOpenSession(ctx) {
		m.Confirmed = true
	}
	return m
}

// missionRun connects the conductor's mission tools (mission_next, mission_assign,
// mission_report, mission_verify, mission_finish, org_spawn's task_id) to the profile's
// task store. It is nil unless the lead is a confirmed mission planner; the session is
// resolved from the conversation by the conductor, never named by the lead.
func missionRun(m *dynorg.MissionConfig) *dynorg.MissionRun {
	if !m.Planning() {
		return nil
	}
	return &dynorg.MissionRun{Store: m.Store, ProfileID: m.ProfileID}
}

// missionMaxAgents is the per-turn worker cap of a turn: a confirmed mission gets the larger
// mission default when the operator never set --org-max-agents; an operator's value, any
// value, stays authoritative (it is bounded 1-20 where it is set).
func missionMaxAgents(settings coderSettings, m *dynorg.MissionConfig) int {
	if m.Planning() && settings.orgMaxAgentsDefaulted && settings.OrgMaxAgents < dynorg.MissionMaxAgents {
		return dynorg.MissionMaxAgents
	}
	return settings.OrgMaxAgents
}
