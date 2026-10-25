package dynorg

import (
	"context"
	"fmt"
	"time"

	"github.com/monoes/mono-agent/internal/ai/chatevents"
	"github.com/monoes/mono-agent/internal/tasks"
)

// missionWatch makes sure the watcher looks after the session. One goroutine per
// Conductor, ended with it.
func (c *Conductor) missionWatch(sid int64) {
	mc, err := c.missions()
	if err != nil {
		return
	}
	c.mission.mu.Lock()
	c.mission.init()
	c.mission.sessions[sid] = true
	c.mission.mu.Unlock()
	c.mission.once.Do(func() {
		if c.ctx.Err() != nil {
			return
		}
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			tick := time.NewTicker(mc.poll())
			defer tick.Stop()
			for {
				select {
				case <-c.ctx.Done():
					return
				case <-tick.C:
					c.missionTick(mc)
				}
			}
		}()
	})
}

// missionTick is one look at every session the Conductor works: an ended session stops its
// workers, a spent budget halts the mission, and a running session has its workers' leases
// renewed, its overlong runs cut and its orphan claims given back.
func (c *Conductor) missionTick(mc *MissionConfig) {
	c.mission.mu.Lock()
	var sids []int64
	for sid := range c.mission.sessions {
		sids = append(sids, sid)
	}
	c.mission.mu.Unlock()
	for _, sid := range sids {
		se, err := mc.Store.GetSession(c.ctx, mc.ProfileID, sid, missionActor())
		if err != nil {
			continue
		}
		switch {
		case se.State == tasks.SessionStopped || se.State == tasks.SessionFailed || se.State == tasks.SessionDone || se.Archived:
			c.stopSessionWorkers(sid, "the session is "+string(se.State))
		case se.BudgetUSD > 0 && se.SpentUSD >= se.BudgetUSD*budgetHaltAt:
			c.missionHalt(c.ctx, se)
		case se.State == tasks.SessionRunning:
			c.renewLeases(c.ctx, mc, sid)
			c.cutOverlong(mc, sid)
		}
		c.missionReconcile(c.ctx, sid)
	}
}

// missionHalt is the 100 percent stop: the session is paused (only the operator resumes it,
// after raising the budget) and its workers are stopped; their claims are given back.
func (c *Conductor) missionHalt(ctx context.Context, se tasks.Session) {
	mc, err := c.missions()
	if err != nil {
		return
	}
	c.mission.mu.Lock()
	first := !c.mission.halted[se.ID]
	c.mission.halted[se.ID] = true
	c.mission.mu.Unlock()
	if se.State == tasks.SessionRunning {
		_, _ = mc.Store.PauseSession(context.WithoutCancel(ctx), mc.ProfileID, se.ID, missionActor())
	}
	c.stopSessionWorkers(se.ID, "the session's budget is spent")
	if first {
		c.notice("mission.budget_halt", fmt.Sprintf("Mission %q has spent its budget ($%.2f of $%.2f) and is halted. The operator raises the budget and resumes it.",
			se.Title, se.SpentUSD, se.BudgetUSD), chatevents.SeverityError)
	}
}

// stopSessionWorkers stops the running workers on a session's tasks, remembering that the
// mission did it (so their stopping is not counted as their failure).
func (c *Conductor) stopSessionWorkers(sid int64, why string) {
	for _, w := range c.sessionWorkers(sid) {
		c.mission.mu.Lock()
		w.task.stopped = why
		c.mission.mu.Unlock()
		_, _ = c.Stop(w.id)
	}
}

// sessionWorkers are the workers on a session's tasks that still run.
func (c *Conductor) sessionWorkers(sid int64) []*worker {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*worker
	for _, id := range c.order {
		if w := c.workers[id]; w != nil && w.task != nil && w.task.session == sid && running(w.status) {
			out = append(out, w)
		}
	}
	return out
}

// renewLeases renews the claim of every live worker, a third of a lease apart.
func (c *Conductor) renewLeases(ctx context.Context, mc *MissionConfig, sid int64) {
	now := c.cfg.Now()
	for _, w := range c.sessionWorkers(sid) {
		c.mission.mu.Lock()
		due := now.Sub(w.task.renewedAt) >= mc.lease()/3
		c.mission.mu.Unlock()
		if !due {
			continue
		}
		if _, err := mc.Store.SessionClaim(ctx, mc.ProfileID, sid, w.task.task.ID, w.task.actor, mc.lease()); err == nil {
			c.mission.mu.Lock()
			w.task.renewedAt = now
			c.mission.mu.Unlock()
		}
	}
}

// cutOverlong stops a worker that has been working on its task past TaskTimeout.
func (c *Conductor) cutOverlong(mc *MissionConfig, sid int64) {
	now := c.cfg.Now()
	for _, w := range c.sessionWorkers(sid) {
		c.mu.Lock()
		working := w.status == chatevents.AgentWorking
		c.mu.Unlock()
		if !working {
			continue
		}
		c.mission.mu.Lock()
		if w.task.workingSince.IsZero() {
			w.task.workingSince = now
		}
		over := now.Sub(w.task.workingSince) > mc.timeout()
		if over {
			w.task.timedOut = true
		}
		c.mission.mu.Unlock()
		if over {
			_, _ = c.Stop(w.id)
		}
	}
}

// missionReconcile gives back the claims of a session whose worker is gone: the ones this
// Conductor made and no worker holds any more (a crashed or timed-out run), and the ones a
// dead process left, once their lease has ended. The task goes back to Ready with its retry
// count untouched, and the scheduler offers it again.
func (c *Conductor) missionReconcile(ctx context.Context, sid int64) {
	mc, err := c.missions()
	if err != nil {
		return
	}
	held, err := mc.Store.SessionTasks(ctx, mc.ProfileID, sid, tasks.Filter{Statuses: []tasks.Status{tasks.StatusInProgress}, Limit: 500}, missionActor())
	if err != nil {
		return
	}
	for _, t := range held {
		if t.Claim == nil || !isMissionName(t.Claim.By, sid) || c.taskWorker(t.ID) != nil {
			continue
		}
		c.mission.mu.Lock()
		oc, ours := c.mission.owned[t.ID]
		ours = ours && oc.name == t.Claim.By
		pending := c.mission.pending[t.ID]
		c.mission.mu.Unlock()
		switch {
		case pending:
			continue
		case ours && t.Claim.By == missionLeadName:
			continue // the lead is doing it itself; its lease ends if it never finishes
		case ours:
			c.releaseClaim(sid, t.ID, t.Claim.By, "no worker holds this task any more: given back to the scheduler")
		case t.Claim.Stale:
			c.releaseClaim(sid, t.ID, t.Claim.By, "the claim's lease ended and nobody renewed it: given back to the scheduler")
		}
	}
}
