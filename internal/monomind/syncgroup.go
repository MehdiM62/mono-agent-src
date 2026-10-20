package monomind

import (
	"sync"
	"time"
)

// How long Settle waits: up to settleFinish for the syncs to finish, then up
// to settleStart in all for the ones whose monomind process has not started.
var (
	settleFinish = time.Second
	settleStart  = 5 * time.Second
)

// SyncGroup tracks knowledge-graph syncs running in the background, so a
// process about to exit, or to close the database they read, can let them
// land without waiting on monomind itself. The zero value is ready to use.
type SyncGroup struct {
	mu        sync.Mutex
	running   int
	unstarted int
	changed   chan struct{} // closed on the next change; nil when nobody waits
}

// Background tracks the syncs of packages that have no database handle of
// their own to settle (the vault's), so storage's Close can let them land.
var Background SyncGroup

// Go runs fn in the background. fn passes started to
// SyncToKnowledgeGraphStarted; returning counts as started too.
func (g *SyncGroup) Go(fn func(started func())) {
	g.update(1, 1)
	go func() {
		var once sync.Once
		started := func() { once.Do(func() { g.update(0, -1) }) }
		defer g.update(-1, 0)
		defer started()
		fn(started)
	}()
}

// Wait blocks until every sync has finished (a test, before its temporary
// HOME is removed).
func (g *SyncGroup) Wait() {
	g.waitUntil(func() bool { return g.running == 0 }, time.Time{})
}

// Settle waits up to a second for the syncs to finish. Past that it waits
// only for those whose monomind process has not started yet, up to five
// seconds in all: one that has started finishes on its own after this
// process exits, but one that never starts is lost, since nothing sends its
// nodes again. It reports whether every sync finished.
func (g *SyncGroup) Settle() bool {
	begin := time.Now()
	if g.waitUntil(func() bool { return g.running == 0 }, begin.Add(settleFinish)) {
		return true
	}
	g.waitUntil(func() bool { return g.unstarted == 0 }, begin.Add(settleStart))
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.running == 0
}

func (g *SyncGroup) update(running, unstarted int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.running += running
	g.unstarted += unstarted
	if g.changed != nil {
		close(g.changed)
		g.changed = nil
	}
}

// waitUntil waits for done (checked under g.mu) until deadline, or for ever
// when deadline is zero, and reports whether done came true.
func (g *SyncGroup) waitUntil(done func() bool, deadline time.Time) bool {
	for {
		g.mu.Lock()
		if done() {
			g.mu.Unlock()
			return true
		}
		if g.changed == nil {
			g.changed = make(chan struct{})
		}
		changed := g.changed
		g.mu.Unlock()
		if deadline.IsZero() {
			<-changed
			continue
		}
		left := time.Until(deadline)
		if left <= 0 {
			return false
		}
		timer := time.NewTimer(left)
		select {
		case <-changed:
			timer.Stop()
		case <-timer.C:
		}
	}
}
