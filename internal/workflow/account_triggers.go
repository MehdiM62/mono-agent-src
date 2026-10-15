package workflow

import (
	"context"

	"github.com/monoes/mono-agent/internal/account"
)

// A locked account arms no trigger in the daemon (spec D8, section 6.2). Dropping what a trigger
// fires (handleTrigger) is not enough for the triggers that work before they fire: an SSE stream
// holds a connection and reads events, a queue consumer takes messages off its queue and
// acknowledges them, a file watcher polls, and an app webhook is registered with its vendor. So
// while the account is locked the daemon's engine keeps none of them armed: RestoreActiveWorkflows
// and Start arm nothing, and the reconcile loop disarms everything within one interval of a lock
// and arms the active workflows again within one interval of a valid login. Like a daemon that was
// not running, a schedule's ticks missed meanwhile are not made up.

// disarmTriggersIfLocked disarms every trigger when the account is locked and reports whether it
// is. When the account is valid again it clears the mark, and the caller's reconcile pass, which
// then finds no change key remembered, arms every active workflow. Callers hold e.reconciler.mu.
func (e *WorkflowEngine) disarmTriggersIfLocked() bool {
	r := &e.reconciler
	if account.Require(context.Background()) == nil {
		if r.disarmed {
			r.disarmed = false
			e.logger.Info().Msg("engine: the monoes.me login is valid again; arming the active workflows' triggers")
		}
		return false
	}
	if !r.disarmed {
		r.disarmed = true
		e.triggerMgr.DeactivateAll()
		r.keys = nil
		e.logger.Warn().Msg("engine: monoes.me login required; no trigger is armed until a login is valid again")
	}
	return true
}

// triggersLocked is disarmTriggersIfLocked for a caller that does not hold e.reconciler.mu.
func (e *WorkflowEngine) triggersLocked() bool {
	e.reconciler.mu.Lock()
	defer e.reconciler.mu.Unlock()
	return e.disarmTriggersIfLocked()
}
