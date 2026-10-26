package coderhost

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The check-in scheduler. It is deterministic code, not a model that sleeps: at
// each pass it looks at a master's children and, when the settings say it is
// time (or something happened), queues one short turn for the master with a
// digest of what it saw. Everything in the digest comes from the host's own
// records (ids, state words, turn status words, ages); no text an agent wrote
// reaches it, so a child cannot steer its master through its own status.

// MinCheckinGap is the shortest time between two check-ins of one master, event
// triggers included.
const MinCheckinGap = time.Minute

// childFact is what is known of one child at a pass.
type childFact struct {
	ID       string `json:"id"`
	State    State  `json:"state"`
	Queued   int    `json:"queued"`
	LastTurn string `json:"last_turn"`   // id of its newest turn
	Status   string `json:"last_status"` // its status: active, completed, failed, cancelled, interrupted
	Silent   bool   `json:"silent"`      // running with no journal event for silent_minutes
	SilentMi int    `json:"-"`
}

type snapshot struct {
	Children []childFact `json:"children"`
}

// checkinPass looks at every master once. It never runs a turn: it queues a send.
func (h *Host) checkinPass(now time.Time) {
	sessions, err := h.Store.ListSessions("")
	if err != nil {
		h.logf("coder host: check-in: %v", err)
		return
	}
	kids := map[string][]Session{}
	for _, x := range sessions {
		if x.Parent != "" && x.State != StateStopped {
			kids[x.ProfileID+"\x00"+x.Parent] = append(kids[x.ProfileID+"\x00"+x.Parent], x)
		}
	}
	for _, m := range sessions {
		children := kids[m.ProfileID+"\x00"+m.ConversationID]
		if m.Role != "master" || len(children) == 0 {
			continue
		}
		h.checkinOne(now, m, children)
	}
}

func (h *Host) checkinOne(now time.Time, m Session, children []Session) {
	// The session has to be free: a turn running, a pause, a question waiting or a
	// command queued all mean the master is being dealt with already. Nothing is
	// recorded, so it is looked at again on the next pass (one catch-up, never a pile).
	if !m.Unattended || m.State != StateIdle || m.TurnID != "" || h.Store.PendingFor(m.ProfileID, m.ConversationID) > 0 {
		return
	}
	settings, err := h.Store.GetSettings(m.ProfileID, m.ConversationID)
	if err != nil {
		return
	}
	prev, err := h.Store.GetCheckin(m.ProfileID, m.ConversationID)
	if err != nil {
		return
	}
	cur := h.snapshotOf(now, m.ProfileID, children, settings.SilentMinutes)
	curJSON, _ := json.Marshal(cur)
	nowStr := now.UTC().Format(timeLayout)

	last, _ := time.Parse(timeLayout, prev.At)
	if prev.At == "" {
		// First sight of this master: remember its children, check in after one interval.
		_ = h.Store.SaveCheckin(m.ProfileID, m.ConversationID, Checkin{At: nowStr, Snapshot: string(curJSON)})
		return
	}
	if now.Sub(last) < MinCheckinGap || !withinActiveHours(settings.ActiveHours, now) {
		return
	}
	var before snapshot
	_ = json.Unmarshal([]byte(prev.Snapshot), &before)
	events := triggeredEvents(before, cur, settings.CheckinTriggers)
	due := settings.CheckinMinutes > 0 && now.Sub(last) >= time.Duration(settings.CheckinMinutes)*time.Minute
	changed := string(curJSON) != prev.Snapshot

	switch {
	case len(events) > 0:
	case due && (!settings.SkipIfUnchanged || changed):
	case due:
		// Nothing changed and the settings say skip: this counts as a look.
		_ = h.Store.SaveCheckin(m.ProfileID, m.ConversationID, Checkin{At: nowStr, Snapshot: string(curJSON)})
		return
	default:
		return
	}
	reason := "scheduled"
	if len(events) > 0 {
		reason = strings.Join(events, ", ")
	}
	turn := make([]byte, 8)
	_, _ = rand.Read(turn)
	if _, err := h.Store.Enqueue(Command{
		ProfileID: m.ProfileID, ConversationID: m.ConversationID, Kind: KindSend,
		TurnID: "checkin-" + hex.EncodeToString(turn), Message: digestText(reason, cur, now),
	}); err != nil {
		h.logf("coder host: check-in for %s: %v", m.ConversationID, err)
		return
	}
	_ = h.Store.SaveCheckin(m.ProfileID, m.ConversationID, Checkin{At: nowStr, Snapshot: string(curJSON)})
}

func (h *Host) snapshotOf(now time.Time, profileID string, children []Session, silentMinutes int) snapshot {
	var out snapshot
	for _, c := range children {
		id, status := h.Store.LastTurn(profileID, c.ConversationID)
		f := childFact{ID: c.ConversationID, State: c.State, Queued: h.Store.PendingFor(profileID, c.ConversationID), LastTurn: id, Status: status}
		if c.State == StateRunning {
			since := h.Store.LastActivity(profileID, c.ConversationID)
			if since.IsZero() {
				since, _ = time.Parse(timeLayout, c.UpdatedAt)
			}
			if !since.IsZero() {
				f.SilentMi = int(now.Sub(since) / time.Minute)
				f.Silent = f.SilentMi >= silentMinutes
			}
		}
		out.Children = append(out.Children, f)
	}
	sort.Slice(out.Children, func(i, j int) bool { return out.Children[i].ID < out.Children[j].ID })
	return out
}

// triggeredEvents names the events that happened between two snapshots and that
// the settings ask to check in on. child_question and budget_80 are settings too,
// but nothing records a question or a spend yet, so they never fire.
func triggeredEvents(before, now snapshot, enabled []string) []string {
	on := map[string]bool{}
	for _, e := range enabled {
		on[e] = true
	}
	old := map[string]childFact{}
	for _, c := range before.Children {
		old[c.ID] = c
	}
	found := map[string]bool{}
	for _, c := range now.Children {
		o, known := old[c.ID]
		if !known {
			continue
		}
		newTurn := c.LastTurn != "" && c.LastTurn != o.LastTurn
		switch {
		case newTurn && c.Status == "completed":
			found["child_finished"] = true
		case newTurn && (c.Status == "failed" || c.Status == "interrupted"):
			found["child_failed"] = true
		}
		if (c.State == StateWaitingUser || c.State == StatePaused) && o.State != c.State {
			found["child_blocked"] = true
		}
		if c.Silent && !o.Silent {
			found["child_silent"] = true
		}
	}
	var out []string
	for e := range found {
		if on[e] {
			out = append(out, e)
		}
	}
	sort.Strings(out)
	return out
}

// digestText is the master's prompt for a check-in: facts only.
func digestText(reason string, s snapshot, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[CHECK-IN] Progress check (%s). The sessions you coordinate, as the host sees them now:\n", reason)
	for _, c := range s.Children {
		last := c.Status
		if last == "" {
			last = "no turn yet"
		}
		fmt.Fprintf(&b, "- %s: %s; last turn %s; %d message(s) queued", c.ID, c.State, last, c.Queued)
		if c.Silent {
			fmt.Fprintf(&b, "; no activity for %d min", c.SilentMi)
		}
		b.WriteString("\n")
	}
	b.WriteString("Review them and give each the direction it needs. Do not redo finished work. This message was written by the host, not by the person or by any of these sessions.")
	_ = now
	return b.String()
}

// withinActiveHours says whether now falls in "HH:MM-HH:MM" (local time; a window
// that crosses midnight works); "" is always.
func withinActiveHours(window string, now time.Time) bool {
	if window == "" {
		return true
	}
	var sh, sm, eh, em int
	if _, err := fmt.Sscanf(window, "%d:%d-%d:%d", &sh, &sm, &eh, &em); err != nil {
		return true
	}
	local := now.Local()
	cur := local.Hour()*60 + local.Minute()
	start, end := sh*60+sm, eh*60+em
	if start <= end {
		return cur >= start && cur < end
	}
	return cur >= start || cur < end
}
