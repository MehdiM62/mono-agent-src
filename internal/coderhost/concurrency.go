package coderhost

// max_concurrent: how many of one master's children may run a turn at once. It
// is counted over the master's subtree (its children; a child has none of its
// own), from the turns the host has live. A send that would start one more stays
// pending, in order, and goes ahead when a sibling's turn ends. It holds every
// send to a child, whoever queued it: the cap bounds what runs, not who asked.

// childRunning counts, per master, the children with a live turn. It is built
// once per pass and kept up to date as the pass starts turns. Callers hold h.mu.
type childRunning map[string]int

func (h *Host) runningChildren() childRunning {
	out := childRunning{}
	sessions, err := h.Store.ListSessions("")
	if err != nil {
		return out
	}
	for _, s := range sessions {
		if s.Role == "child" && s.Parent != "" && h.live[key(s.ProfileID, s.ConversationID)] != nil {
			out[key(s.ProfileID, s.Parent)]++
		}
	}
	return out
}

// childSlotFull says whether x is a child whose master already has max_concurrent
// children running turns. A session that is not a child is never held.
func (h *Host) childSlotFull(x Session, running childRunning) bool {
	if x.Role != "child" || x.Parent == "" {
		return false
	}
	settings, _ := h.Store.GetSettings(x.ProfileID, x.Parent) // unreadable settings give the defaults
	return running[key(x.ProfileID, x.Parent)] >= settings.MaxConcurrent
}

func (r childRunning) started(x Session) {
	if x.Role == "child" && x.Parent != "" {
		r[key(x.ProfileID, x.Parent)]++
	}
}
