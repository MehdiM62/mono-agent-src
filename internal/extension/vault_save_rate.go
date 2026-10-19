package extension

import (
	"sync"
	"time"
)

// vaultSaveRate is how many vault.save requests one profile may make a
// minute, counted in this bridge process; vaultSaveNow is its clock. Both are
// read when SetVaultSink installs the method (tests lower them first).
var (
	vaultSaveRate = 20
	vaultSaveNow  = time.Now
)

// saveRateLimiter is a sliding one-minute window per profile.
type saveRateLimiter struct {
	mu   sync.Mutex
	per  int
	now  func() time.Time
	seen map[string][]time.Time
}

func newSaveRateLimiter(per int, now func() time.Time) *saveRateLimiter {
	return &saveRateLimiter{per: per, now: now, seen: map[string][]time.Time{}}
}

// allow counts one save for profile, or says how long until one fits. Windows
// with nothing left in them are dropped on every call, so a stream of made-up
// profile ids cannot grow the map past a minute's worth.
func (l *saveRateLimiter) allow(profile string) (wait time.Duration, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-time.Minute)
	for p, ts := range l.seen {
		i := 0
		for i < len(ts) && !ts[i].After(cutoff) {
			i++
		}
		if i == len(ts) {
			delete(l.seen, p)
		} else {
			l.seen[p] = ts[i:]
		}
	}
	ts := l.seen[profile]
	if len(ts) >= l.per {
		return ts[0].Add(time.Minute).Sub(now), false
	}
	l.seen[profile] = append(ts, now)
	return 0, true
}
