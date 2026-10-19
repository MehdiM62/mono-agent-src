package extension

import (
	"context"
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

// liveProfileCache answers "is this profile live?" from a list read at most
// once a liveProfileTTL, so a stream of made-up ids costs one list a second.
type liveProfileCache struct {
	mu   sync.Mutex
	read func(context.Context) ([]string, error)
	now  func() time.Time
	at   time.Time // the last read, successful or not
	ids  map[string]bool
	err  error // the last read's failure, while no list was ever read
}

func newLiveProfileCache(read func(context.Context) ([]string, error), now func() time.Time) *liveProfileCache {
	return &liveProfileCache{read: read, now: now}
}

// has reports whether id is a live profile. The list is read again only after
// liveProfileTTL, whether the last read worked or not; a failed read keeps the
// list read before it, and with none yet its error is the answer.
func (c *liveProfileCache) has(ctx context.Context, id string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now := c.now(); c.at.IsZero() || now.Sub(c.at) >= liveProfileTTL {
		c.at = now
		list, err := c.read(ctx)
		if err == nil {
			c.ids, c.err = make(map[string]bool, len(list)), nil
			for _, p := range list {
				c.ids[p] = true
			}
		} else if c.ids == nil {
			c.err = err
		}
	}
	if c.ids == nil {
		return false, c.err
	}
	return c.ids[id], nil
}
