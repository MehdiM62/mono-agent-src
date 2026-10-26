package coderhost

import (
	"encoding/json"
	"sort"
	"time"
)

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

// childRunning is what one pass knows about the cap: per master, how many children
// run a turn, and which children have a send waiting, oldest first. A child may start
// only when the children waiting ahead of it plus those running leave room, so the
// freed slot goes to the oldest waiting send of the master, not to whoever the pass
// reaches first. Built once per pass and kept up to date as the pass starts turns.
// Callers hold h.mu.
type childRunning struct {
	n       map[string]int
	waiting map[string][]string // master -> chats with a waiting send, by the id of their oldest
	err     error
}

func (h *Host) runningChildren(cmds []Command) *childRunning {
	out := &childRunning{n: map[string]int{}, waiting: map[string][]string{}}
	sessions, err := h.Store.ListSessions("")
	if err != nil {
		out.err = err
		return out
	}
	by := map[string]Session{}
	for _, s := range sessions {
		by[key(s.ProfileID, s.ConversationID)] = s
		if s.Role == "child" && s.Parent != "" && h.live[key(s.ProfileID, s.ConversationID)] != nil {
			out.n[key(s.ProfileID, s.Parent)]++
		}
	}
	oldest := map[string]int64{} // chat -> id of its first waiting send
	for _, c := range cmds {
		k := key(c.ProfileID, c.ConversationID)
		x, ok := by[k]
		if c.Kind != KindSend || !ok || x.Role != "child" || x.Parent == "" || h.live[k] != nil || x.State == StatePaused || x.State == StateStopped {
			continue
		}
		if c.Issuer != "" { // a master's send its paused or stopped master holds is not waiting for a slot
			if allowed, hold, _ := h.issuerStillAllowed(c, x); !allowed || hold {
				continue
			}
		}
		if id, seen := oldest[k]; !seen || c.ID < id {
			oldest[k] = c.ID
		}
	}
	for k := range oldest {
		m := key(by[k].ProfileID, by[k].Parent)
		out.waiting[m] = append(out.waiting[m], k)
	}
	for m := range out.waiting {
		w := out.waiting[m]
		sort.Slice(w, func(i, j int) bool { return oldest[w[i]] < oldest[w[j]] })
	}
	return out
}

// childSlotFull says whether chat k (session x) must wait: x is a child whose master
// has max_concurrent turns running, or children with older waiting sends that take
// the free slots first. A session that is not a child is never held.
func (h *Host) childSlotFull(x Session, k string, r *childRunning) bool {
	if x.Role != "child" || x.Parent == "" {
		return false
	}
	m := key(x.ProfileID, x.Parent)
	if r.err != nil {
		h.logCapUnreadable("running-children", r.err)
		return true
	}
	settings, err := h.Store.GetSettings(x.ProfileID, x.Parent)
	if err != nil {
		h.logCapUnreadable(m, err)
		return true
	}
	ahead := len(r.waiting[m])
	for i, w := range r.waiting[m] {
		if w == k {
			ahead = i
			break
		}
	}
	return r.n[m]+ahead >= settings.MaxConcurrent
}

// logCapUnreadable says, at most once an hour for a master, that its children's sends
// are held because the cap could not be read.
func (h *Host) logCapUnreadable(what string, err error) {
	now := h.Store.now()
	if last, ok := h.capLogged[what]; ok && now.Sub(last) < time.Hour {
		return
	}
	if h.capLogged == nil {
		h.capLogged = map[string]time.Time{}
	}
	h.capLogged[what] = now
	h.logf("coder host: sends are held because max_concurrent cannot be read (%v); trying again on the next pass", err)
}

func (r *childRunning) started(x Session, k string) {
	if x.Role != "child" || x.Parent == "" {
		return
	}
	m := key(x.ProfileID, x.Parent)
	r.n[m]++
	w := r.waiting[m]
	for i := range w {
		if w[i] == k {
			r.waiting[m] = append(w[:i:i], w[i+1:]...)
			return
		}
	}
}

// pendingOrdered is Pending with the commands of the chats the cap held on the last
// pass (except a stop) moved to the end, in id order among themselves: a pile of
// held sends (up to 50 a chat) must not fill the window and hide a stop or another
// chat's send behind them. A stop goes ahead because it cancels the chat's earlier
// sends whichever order they are looked at in; a pause or resume stays in its place
// among the chat's own sends.
func (h *Host) pendingOrdered() ([]Command, error) {
	if len(h.capHeld) == 0 {
		return h.Store.Pending()
	}
	pairs := make([][2]string, 0, len(h.capHeld))
	for _, p := range h.capHeld {
		pairs = append(pairs, p)
	}
	js, _ := json.Marshal(pairs)
	const in = ` (kind != 'stop' AND (profile_id, conversation_id) IN (SELECT json_extract(value, '$[0]'), json_extract(value, '$[1]') FROM json_each(?)))`
	rest, err := h.Store.pendingWhere(` AND NOT`+in, []any{string(js)})
	if err != nil {
		return nil, err
	}
	held, err := h.Store.pendingWhere(` AND`+in, []any{string(js)})
	if err != nil {
		return nil, err
	}
	return append(rest, held...), nil
}
