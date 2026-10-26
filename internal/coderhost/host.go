package coderhost

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Turn is a running turn process.
type Turn interface {
	PID() int
	// Stamp says what makes PID this process and not a later one that reused
	// the number ("" when it cannot be told).
	Stamp() string
	// Wait blocks until the process ends.
	Wait() error
	// Stop asks the process (and its children) to end, then forces it.
	Stop()
}

// Launcher starts and finds turn processes. A turn is a `chat --conversation`
// process that journals itself; it must not die with the host.
type Launcher interface {
	Start(profileID, conversationID, turnID, message string, attachments []string) (Turn, error)
	// Adopt returns the turn process pid/stamp still names, or nil when it is gone.
	Adopt(pid int, stamp string) Turn
	// Finish closes a turn that ended without finishing itself. It is a no-op
	// for a turn that did.
	Finish(profileID, conversationID, turnID, reason string)
}

// Host carries commands out. Run it from one process only (the caller holds
// the single-instance lock).
type Host struct {
	Store *Store
	L     Launcher
	Poll  time.Duration
	Logf  func(format string, args ...any)

	// Mesh, when set, keeps the sessions visible on monomind's mesh and hands waiting
	// messages to the next turn (see Mesh). MeshEvery is how often it is brought up
	// to date (default 15 s); a session idle for MeshIdle (default 6 h) leaves.
	Mesh      Mesh
	MeshEvery time.Duration
	MeshIdle  time.Duration

	mu   sync.Mutex // serializes session changes between the loop and the turn watchers
	live map[string]Turn
	wg   sync.WaitGroup
}

func key(profileID, conversationID string) string { return profileID + "\x00" + conversationID }

func (h *Host) logf(format string, args ...any) {
	if h.Logf != nil {
		h.Logf(format, args...)
	}
}

// Recover reattaches to turn processes that outlived a previous host and closes
// the sessions whose process is gone.
func (h *Host) Recover() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.live == nil {
		h.live = map[string]Turn{}
	}
	sessions, err := h.Store.ListSessions("")
	if err != nil {
		return err
	}
	for _, x := range sessions {
		if x.TurnID == "" && x.TurnPID == 0 && x.State != StateRunning {
			continue
		}
		if t := h.L.Adopt(x.TurnPID, x.TurnStamp); t != nil && x.TurnPID > 0 {
			h.live[key(x.ProfileID, x.ConversationID)] = t
			h.watch(x, t)
			continue
		}
		if x.TurnID != "" {
			h.L.Finish(x.ProfileID, x.ConversationID, x.TurnID, "the coder host restarted and the turn's process was gone")
		}
		h.settle(x)
	}
	return nil
}

// settle clears a session's turn; a paused session stays paused. Callers hold mu.
func (h *Host) settle(x Session) {
	if cur, ok, err := h.Store.GetSession(x.ProfileID, x.ConversationID); err == nil && ok {
		x = cur
	}
	x.TurnID, x.TurnPID, x.TurnStamp = "", 0, ""
	if x.State != StatePaused {
		x.State = StateIdle
	}
	if err := h.Store.SaveSession(x); err != nil {
		h.logf("coder host: saving %s: %v", x.ConversationID, err)
	}
}

// watch waits for the turn to end in the background.
func (h *Host) watch(x Session, t Turn) {
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		_ = t.Wait()
		h.L.Finish(x.ProfileID, x.ConversationID, x.TurnID, "the turn's process ended without finishing the turn")
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.live, key(x.ProfileID, x.ConversationID))
		h.settle(x)
	}()
}

// Tick makes one pass over the waiting commands. A command that cannot run yet
// (a send to a busy or paused session) stays pending, in order.
func (h *Host) Tick() error {
	if err := h.Store.ExpireSends(); err != nil {
		return err
	}
	cmds, err := h.Store.Pending()
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.live == nil {
		h.live = map[string]Turn{}
	}
	blocked := map[string]bool{} // an earlier send for the chat is still waiting: later sends keep their order
	for _, c := range cmds {
		k := key(c.ProfileID, c.ConversationID)
		x, err := h.Store.EnsureSession(c.ProfileID, c.ConversationID)
		if err != nil {
			return err
		}
		switch c.Kind {
		case KindSend:
			if blocked[k] || h.live[k] != nil || x.State == StatePaused {
				blocked[k] = true
				continue
			}
			if x.State == StateStopped {
				h.end(c, CmdFailed, "the session is stopped")
				continue
			}
			// The turn is recorded before it starts, without a pid yet, so the
			// app's startup sweep sees it as the host's while it is admitted.
			x.State, x.TurnID, x.TurnPID, x.TurnStamp = StateRunning, c.TurnID, 0, ""
			if err := h.Store.SaveSession(x); err != nil {
				h.end(c, CmdFailed, "could not record the session: "+err.Error())
				return err
			}
			h.mu.Unlock() // admission can take seconds; do not hold up the turn watchers meanwhile
			message, commit := c.Message, func() {}
			if h.Mesh != nil {
				if ms, ok := h.meshSessionFor(c.ProfileID, c.ConversationID); ok {
					block, done := h.Mesh.Inbox(ms)
					// Peer content goes ahead of the person's message, sealed as data, but never at the
					// cost of the message: a block that would push the prompt over its limit stays unread.
					if block != "" && len(block)+2+len(message) <= MaxMessageBytes {
						message, commit = block+"\n\n"+message, done
					}
				}
			}
			t, err := h.L.Start(c.ProfileID, c.ConversationID, c.TurnID, message, c.Attachments)
			h.mu.Lock()
			if err != nil {
				h.settle(x)
				h.end(c, CmdFailed, err.Error())
				continue
			}
			commit() // the turn has the messages now: mark them read
			x.TurnPID, x.TurnStamp = t.PID(), t.Stamp()
			if err := h.Store.SaveSession(x); err != nil {
				t.Stop()
				h.end(c, CmdFailed, "could not record the session: "+err.Error())
				return err
			}
			h.live[k] = t
			h.watch(x, t)
			h.end(c, CmdDone, "")
		case KindStop:
			// A stop also drops the sends queued before it: the operator
			// stopped the session, not just the turn that happened to run.
			dropped, err := h.Store.CancelPendingSends(c.ProfileID, c.ConversationID, c.ID, "cancelled by a stop")
			if err != nil {
				return err
			}
			t := h.live[k]
			if t == nil && dropped == 0 {
				h.end(c, CmdFailed, "no turn is running and nothing is queued")
				continue
			}
			if t != nil {
				t.Stop()
			}
			h.end(c, CmdDone, "")
		case KindPause:
			x.State = StatePaused
			if err := h.Store.SaveSession(x); err != nil {
				return err
			}
			h.end(c, CmdDone, "")
		case KindResume:
			if x.State == StatePaused {
				x.State = StateIdle
				if h.live[k] != nil {
					x.State = StateRunning
				}
				if err := h.Store.SaveSession(x); err != nil {
					return err
				}
			}
			h.end(c, CmdDone, "")
		default:
			h.end(c, CmdFailed, fmt.Sprintf("unknown command %q", c.Kind))
		}
	}
	return nil
}

func (h *Host) end(c Command, status, errText string) {
	if err := h.Store.SetCommand(c.ID, status, errText); err != nil {
		h.logf("coder host: recording command %d: %v", c.ID, err)
	}
}

// Run recovers, then ticks until ctx ends. Turn processes keep running when it
// returns: that is the point of the host.
func (h *Host) Run(ctx context.Context) error {
	if err := h.Recover(); err != nil {
		return err
	}
	poll := h.Poll
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}
	if h.Mesh != nil {
		meshDone := make(chan struct{})
		go func() {
			defer close(meshDone)
			h.meshLoop(ctx)
		}()
		defer func() { <-meshDone; h.Mesh.Close() }()
	}
	tk := time.NewTicker(poll)
	defer tk.Stop()
	for {
		if err := h.Tick(); err != nil {
			h.logf("coder host: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tk.C:
		}
	}
}

// Wait blocks until every turn watcher has finished (tests).
func (h *Host) Wait() { h.wg.Wait() }

// meshLoop brings the mesh up to date now and every MeshEvery until ctx ends.
func (h *Host) meshLoop(ctx context.Context) {
	every := h.MeshEvery
	if every <= 0 {
		every = 15 * time.Second
	}
	for {
		h.meshSync(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// meshSync tells the mesh which sessions to show: those whose settings make them
// visible and that are not stopped, are doing something, or were touched within
// MeshIdle. What it says about a session is generic (its state and access level),
// never anything from its prompts.
func (h *Host) meshSync(ctx context.Context) {
	idle := h.MeshIdle
	if idle <= 0 {
		idle = 6 * time.Hour
	}
	sessions, err := h.Store.ListSessions("")
	if err != nil {
		h.logf("coder host: mesh: %v", err)
		return
	}
	children := map[string][]string{}
	for _, x := range sessions {
		if x.Parent != "" {
			children[x.Parent] = append(children[x.Parent], x.ConversationID)
		}
	}
	var want []MeshSession
	for _, x := range sessions {
		active := x.State == StateRunning || x.State == StateWaitingUser || x.State == StatePaused
		if x.State == StateStopped {
			continue
		}
		if t, err := time.Parse(timeLayout, x.UpdatedAt); !active && (err != nil || h.Store.now().Sub(t) > idle) {
			continue
		}
		settings, err := h.Store.GetSettings(x.ProfileID, x.ConversationID)
		if err != nil {
			return // leave the mesh as it is: dropping the session for a read error would make it leave and rejoin
		}
		if !settings.MeshVisible {
			continue
		}
		cwd, _, ok, err := h.Store.ConversationInfo(x.ProfileID, x.ConversationID)
		if err != nil {
			return
		}
		if !ok || cwd == "" {
			continue
		}
		started, _ := time.Parse(timeLayout, x.CreatedAt)
		related := append([]string{}, children[x.ConversationID]...)
		if x.Parent != "" {
			related = append(related, x.Parent)
		}
		want = append(want, MeshSession{
			ProfileID: x.ProfileID, ConversationID: x.ConversationID, Cwd: cwd, Started: started.Local(),
			Task: string(x.State), Alias: settings.MeshAlias, // nothing about its work, its folder's path or its access level
			Accept: settings.MeshAccept, Related: related,
		})
	}
	h.Mesh.Sync(ctx, want)
}

// meshSessionFor is the mesh's view of one session as it is right now, for reading
// its inbox with the scope it has now and not the one of the last pass.
func (h *Host) meshSessionFor(profileID, conversationID string) (MeshSession, bool) {
	settings, err := h.Store.GetSettings(profileID, conversationID)
	if err != nil || !settings.MeshVisible || settings.MeshAccept == "none" {
		return MeshSession{}, false
	}
	related := []string{}
	if sessions, err := h.Store.ListSessions(profileID); err == nil {
		for _, x := range sessions {
			switch {
			case x.Parent == conversationID:
				related = append(related, x.ConversationID)
			case x.ConversationID == conversationID && x.Parent != "":
				related = append(related, x.Parent)
			}
		}
	}
	return MeshSession{ProfileID: profileID, ConversationID: conversationID, Accept: settings.MeshAccept, Related: related}, true
}
