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
			t, err := h.L.Start(c.ProfileID, c.ConversationID, c.TurnID, c.Message, c.Attachments)
			if err != nil {
				h.end(c, CmdFailed, err.Error())
				continue
			}
			x.State, x.TurnID, x.TurnPID, x.TurnStamp = StateRunning, c.TurnID, t.PID(), t.Stamp()
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
