package coderhost

import "encoding/json"

// max_concurrent: how many of one master's children may run a turn at once. It
// is counted over the master's subtree (its children; a child has none of its
// own), from the turns this host has live (started by it, or adopted by Recover);
// a turn another process runs is not counted. A send that would start one more
// stays pending, in order, and goes ahead when a sibling's turn ends. It holds
// every send to a child, whoever queued it: the cap bounds what runs, not who
// asked. A held send can expire like any other (15 minutes for a master's, 6
// hours for the rest) and then fails with the expiry reason. When the count or
// the master's settings cannot be read the cap holds (fail closed): the send is
// tried again on the next pass.

// childRunning counts, per master, the children with a live turn. It is built
// once per pass and kept up to date as the pass starts turns. Callers hold h.mu.
type childRunning struct {
	n   map[string]int
	err error
}

func (h *Host) runningChildren() *childRunning {
	out := &childRunning{n: map[string]int{}}
	sessions, err := h.Store.ListSessions("")
	if err != nil {
		out.err = err
		return out
	}
	for _, s := range sessions {
		if s.Role == "child" && s.Parent != "" && h.live[key(s.ProfileID, s.ConversationID)] != nil {
			out.n[key(s.ProfileID, s.Parent)]++
		}
	}
	return out
}

// childSlotFull says whether x is a child whose master already has max_concurrent
// children running turns. A session that is not a child is never held.
func (h *Host) childSlotFull(x Session, r *childRunning) bool {
	if x.Role != "child" || x.Parent == "" {
		return false
	}
	if r.err != nil {
		return true
	}
	settings, err := h.Store.GetSettings(x.ProfileID, x.Parent)
	if err != nil {
		return true
	}
	return r.n[key(x.ProfileID, x.Parent)] >= settings.MaxConcurrent
}

func (r *childRunning) started(x Session) {
	if x.Role == "child" && x.Parent != "" {
		r.n[key(x.ProfileID, x.Parent)]++
	}
}

// pendingOrdered is Pending with the sends of the chats the cap held on the last
// pass moved to the end: a pile of held sends (up to 50 a chat) must not fill the
// window and hide a stop or another chat's send behind them. A chat's sends stay
// in order among themselves.
func (h *Host) pendingOrdered() ([]Command, error) {
	if len(h.capHeld) == 0 {
		return h.Store.Pending()
	}
	pairs := make([][2]string, 0, len(h.capHeld))
	for _, p := range h.capHeld {
		pairs = append(pairs, p)
	}
	js, _ := json.Marshal(pairs)
	const in = ` (profile_id, conversation_id) IN (SELECT json_extract(value, '$[0]'), json_extract(value, '$[1]') FROM json_each(?))`
	rest, err := h.Store.pendingWhere(` AND NOT (kind = 'send' AND`+in+`)`, []any{string(js)})
	if err != nil {
		return nil, err
	}
	held, err := h.Store.pendingWhere(` AND kind = 'send' AND`+in, []any{string(js)})
	if err != nil {
		return nil, err
	}
	return append(rest, held...), nil
}
