package extension

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// vault.save's rate limits, counted in this bridge process: vaultSaveRate
// saves a minute for one profile and vaultSaveTotalRate in all, on the clock
// vaultSaveNow. They are read when SetVaultSink installs the method (tests
// lower them first).
var (
	vaultSaveRate      = 20
	vaultSaveTotalRate = 60
	vaultSaveNow       = time.Now
)

// rateSweepEvery is how often allow drops the windows of every profile, not
// only the one it was asked about.
const rateSweepEvery = 10 * time.Second

// saveRateLimiter is a sliding one-minute window per profile and one for all
// of them. Only live profiles reach it (liveProfileCache), so its map holds at
// most one window per profile that exists.
type saveRateLimiter struct {
	mu         sync.Mutex
	per, total int
	now        func() time.Time
	seen       map[string][]time.Time
	all        []time.Time
	swept      time.Time
}

func newSaveRateLimiter(per, total int, now func() time.Time) *saveRateLimiter {
	return &saveRateLimiter{per: per, total: total, now: now, seen: map[string][]time.Time{}}
}

// allow counts one save for profile, or says how long until one fits and
// whether it is the overall window that is full.
func (l *saveRateLimiter) allow(profile string) (wait time.Duration, overall, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-time.Minute)
	if now.Sub(l.swept) >= rateSweepEvery {
		for p, ts := range l.seen {
			if ts = dropBefore(ts, cutoff); len(ts) == 0 {
				delete(l.seen, p)
			} else {
				l.seen[p] = ts
			}
		}
		l.swept = now
	}
	l.all = dropBefore(l.all, cutoff)
	ts := dropBefore(l.seen[profile], cutoff)
	switch {
	case len(l.all) >= l.total:
		l.seen[profile] = ts
		return l.all[0].Add(time.Minute).Sub(now), true, false
	case len(ts) >= l.per:
		l.seen[profile] = ts
		return ts[0].Add(time.Minute).Sub(now), false, false
	}
	l.all = append(l.all, now)
	l.seen[profile] = append(ts, now)
	return 0, false, true
}

// dropBefore is ts (oldest first) without the times at or before cutoff.
func dropBefore(ts []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(ts) && !ts[i].After(cutoff) {
		i++
	}
	return ts[i:]
}

// liveProfileTTL is how long liveProfileCache trusts its list.
const liveProfileTTL = time.Second

// liveProfileReadTimeout bounds one read of the list. The read is the
// cache's, not the request's that started it: it runs without that
// request's cancellation, so a request that gives up does not fail the
// others waiting on the same read.
const liveProfileReadTimeout = 2 * time.Second

// liveProfileCache answers "is this profile live?" from a list read at most
// once a liveProfileTTL, so a stream of made-up ids costs one list a second.
// A profile made since the last read is not in the list yet: its save is
// refused (invalid_input) until the next read, at most liveProfileTTL later.
type liveProfileCache struct {
	mu   sync.Mutex
	read func(context.Context) ([]string, error)
	now  func() time.Time
	at   time.Time     // when the last read started, successful or not
	done chan struct{} // closed when the read in flight ends; nil with none in flight
	ids  map[string]bool
	err  error // the last read's failure, while no list was ever read
}

func newLiveProfileCache(read func(context.Context) ([]string, error), now func() time.Time) *liveProfileCache {
	return &liveProfileCache{read: read, now: now}
}

// has reports whether id is a live profile. The list is read again only after
// liveProfileTTL, whether the last read worked or not; a failed read keeps the
// list read before it, and with none yet its error is the answer. One read
// runs at a time, outside the lock: a caller that finds one in flight waits
// for its result, or returns at once when its own ctx ends.
func (c *liveProfileCache) has(ctx context.Context, id string) (bool, error) {
	c.mu.Lock()
	if now := c.now(); c.done == nil && (c.at.IsZero() || now.Sub(c.at) >= liveProfileTTL) {
		c.at = now
		c.done = make(chan struct{})
		go c.refresh(context.WithoutCancel(ctx), c.done)
	}
	done := c.done
	c.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ids == nil {
		return false, c.err
	}
	return c.ids[id], nil
}

// refresh reads the list once, within liveProfileReadTimeout, and closes done.
// It runs on its own goroutine, so a panic in read is turned into a failed
// read here rather than taking the bridge down.
func (c *liveProfileCache) refresh(ctx context.Context, done chan struct{}) {
	var (
		list []string
		err  error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("vault.save: listing the profiles panicked: %v", r)
			}
		}()
		ctx, cancel := context.WithTimeout(ctx, liveProfileReadTimeout)
		defer cancel()
		list, err = c.read(ctx)
	}()
	c.mu.Lock()
	if err == nil {
		c.ids, c.err = make(map[string]bool, len(list)), nil
		for _, p := range list {
			c.ids[p] = true
		}
	} else if c.ids == nil {
		c.err = err
	}
	c.done = nil
	c.mu.Unlock()
	close(done)
}
