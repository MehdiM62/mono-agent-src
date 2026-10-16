package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testClaim(path string, alive func(int) bool) *claimLock {
	return &claimLock{
		path: path, alive: alive, staleAfter: time.Hour, emptyGrace: 50 * time.Millisecond, breakAge: time.Hour,
		poll: time.Millisecond, timeout: 5 * time.Second, pid: os.Getpid(),
	}
}

func writeClaim(t *testing.T, path, nonce string, pid int, at time.Time) {
	t.Helper()
	body := fmt.Sprintf("%s %d %d", nonce, pid, at.UnixNano())
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestClaimFromDeadPIDIsBrokenQuickly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.claim")
	writeClaim(t, path, "dead", 99999, time.Now())
	c := testClaim(path, func(int) bool { return false })
	start := time.Now()
	if _, err := c.acquire(); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("took %s to break a dead holder's claim", time.Since(start))
	}
}

func TestClaimOfLiveHolderIsRespected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.claim")
	writeClaim(t, path, "live", 4242, time.Now())
	c := testClaim(path, func(pid int) bool { return pid == 4242 })
	c.timeout = 100 * time.Millisecond
	if _, err := c.acquire(); err == nil {
		t.Fatal("acquired a lock held by a live process")
	}
	if b, _ := os.ReadFile(path); len(b) == 0 {
		t.Fatal("live holder's claim was removed")
	}
}

func TestClaimWithoutRecentHeartbeatIsBrokenEvenIfPIDLooksAlive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.claim")
	writeClaim(t, path, "old", 4242, time.Now().Add(-2*time.Hour))
	c := testClaim(path, func(int) bool { return true })
	if _, err := c.acquire(); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseKeepsAClaimWithAnotherNonce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.claim")
	c := testClaim(path, func(int) bool { return true })
	nonce, err := c.acquire()
	if err != nil {
		t.Fatal(err)
	}
	writeClaim(t, path, "someone-else", 1, time.Now())
	if err := c.release(nonce); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("release removed a claim owned by someone else")
	}
	if err := c.release("someone-else"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("release did not remove our own claim")
	}
}

// A live holder that heartbeats is never judged stale by total age, however
// long it holds the claim.
func TestHeartbeatKeepsALongHeldClaimAlive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.claim")
	holder := testClaim(path, func(int) bool { return true })
	holder.staleAfter = 80 * time.Millisecond
	holder.heartbeat = 5 * time.Millisecond
	nonce, err := holder.acquire()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond) // five times staleAfter

	waiter := testClaim(path, func(int) bool { return true })
	waiter.staleAfter = holder.staleAfter
	waiter.timeout = 100 * time.Millisecond
	if _, err := waiter.acquire(); err == nil {
		t.Fatal("a heartbeating holder lost its claim to a waiter")
	}
	if err := holder.release(nonce); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("release left the claim behind")
	}
}

// A crashed holder whose PID was reused by an unrelated live process leaves
// a claim that looks alive by PID but is no longer heartbeating.
func TestClaimWithLivePIDButStoppedHeartbeatIsBroken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.claim")
	writeClaim(t, path, "crashed", 4242, time.Now())
	c := testClaim(path, func(pid int) bool { return pid == 4242 })
	c.staleAfter = 60 * time.Millisecond
	start := time.Now()
	if _, err := c.acquire(); err != nil {
		t.Fatalf("a claim with a reused live PID and no heartbeat blocked: %v", err)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Fatal("claim was broken before the heartbeat bound elapsed")
	}
}

// The heartbeat must stop refreshing once the claim is no longer ours.
func TestHeartbeatDoesNotRefreshAClaimOwnedBySomeoneElse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.claim")
	c := testClaim(path, func(int) bool { return true })
	c.heartbeat = 5 * time.Millisecond
	nonce, err := c.acquire()
	if err != nil {
		t.Fatal(err)
	}
	stolen := time.Now().Add(-time.Minute)
	writeClaim(t, path, "someone-else", 1, stolen)
	time.Sleep(100 * time.Millisecond)
	want := fmt.Sprintf("someone-else 1 %d", stolen.UnixNano())
	if b, _ := os.ReadFile(path); string(b) != want {
		t.Fatalf("heartbeat overwrote another owner's claim: %q", b)
	}
	if err := c.release(nonce); err != nil {
		t.Fatal(err)
	}
}

// An empty or truncated claim is never judged stale at once (a truncated
// timestamp must not read as 1970); it is broken only after the grace.
func TestEmptyOrTruncatedClaimIsBrokenOnlyAfterGrace(t *testing.T) {
	for name, body := range map[string]string{
		"empty":     "",
		"no-fields": "abcdef",
		"truncated": fmt.Sprintf("abc 4242 %d", 17),
		"cut-pid":   "abc 42",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "db.claim")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			c := testClaim(path, func(int) bool { return false })
			c.emptyGrace = 200 * time.Millisecond
			c.timeout = 50 * time.Millisecond
			if _, err := c.acquire(); err == nil {
				t.Fatal("a just-written unparseable claim was broken at once")
			}
			c.timeout = 5 * time.Second
			start := time.Now()
			if _, err := c.acquire(); err != nil {
				t.Fatalf("an abandoned unparseable claim was never broken: %v", err)
			}
			if time.Since(start) > 2*time.Second {
				t.Fatalf("took %s after the grace", time.Since(start))
			}
		})
	}
}

// A crash between writing the temp file and publishing it leaves only a
// stray temp file, which never blocks anyone.
func TestStrayTempFileNeverBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.claim")
	if err := os.WriteFile(path+".deadbeef.tmp", []byte("abc 4242"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := testClaim(path, func(int) bool { return true })
	c.timeout = 100 * time.Millisecond
	if _, err := c.acquire(); err != nil {
		t.Fatalf("stray temp file blocked the lock: %v", err)
	}
}

// Without hard links the claim is still exclusive and complete.
func TestClaimPublishFallsBackWithoutHardLinks(t *testing.T) {
	old := linkFile
	linkFile = func(string, string) error { return errors.New("hard links not supported") }
	t.Cleanup(func() { linkFile = old })

	path := filepath.Join(t.TempDir(), "db.claim")
	c := testClaim(path, func(int) bool { return true })
	nonce, err := c.acquire()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if got, _, _, ok := parseClaim(string(b)); !ok || got != nonce {
		t.Fatalf("fallback claim body = %q", b)
	}
	other := testClaim(path, func(int) bool { return true })
	other.timeout = 50 * time.Millisecond
	if _, err := other.acquire(); err == nil {
		t.Fatal("fallback publish was not exclusive")
	}
}

// A reader polling the claim while holders come and go never sees a claim
// without its complete body.
func TestClaimIsNeverVisibleHalfWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.claim")
	stop := make(chan struct{})
	var bad atomic.Value
	var rd sync.WaitGroup
	rd.Add(1)
	go func() {
		defer rd.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			b, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if _, _, _, ok := parseClaim(string(b)); !ok {
				bad.Store(string(b))
			}
		}
	}()
	c := testClaim(path, func(int) bool { return true })
	for i := 0; i < 300; i++ {
		nonce, err := c.acquire()
		if err != nil {
			t.Fatal(err)
		}
		if err := c.release(nonce); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	rd.Wait()
	if v := bad.Load(); v != nil {
		t.Fatalf("observed a partial claim %q", v)
	}
}

func TestRemoveIfMatchesLeavesAReplacedFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.break")
	if err := os.WriteFile(path, []byte("new-owner"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := testClaim(path, func(int) bool { return true })
	if c.removeIfMatches(path, "the-stale-one-we-judged") {
		t.Fatal("removed a file that no longer matches what was judged")
	}
	if b, _ := os.ReadFile(path); string(b) != "new-owner" {
		t.Fatalf("file not restored intact: %q", b)
	}
	if !c.removeIfMatches(path, "new-owner") {
		t.Fatal("did not remove a matching file")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("matching file still present")
	}
}

func TestUnlockBreakKeepsAnotherOwnersBreakLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.claim")
	c := testClaim(path, func(int) bool { return true })
	mine, ok := c.tryBreakLock()
	if !ok {
		t.Fatal("could not take a free break lock")
	}
	if err := os.WriteFile(path+".break", []byte("someone else"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.unlockBreak(mine)
	if b, _ := os.ReadFile(path + ".break"); string(b) != "someone else" {
		t.Fatalf("unlock removed a break lock with a different owner: %q", b)
	}
}

func TestBreakLockOfDeadOwnerIsBrokenAndLiveOneRespected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db.claim")
	writeClaim(t, path, "dead-claim", 99999, time.Now())
	writeClaim(t, path+".break", "dead-breaker", 99999, time.Now())
	c := testClaim(path, func(pid int) bool { return pid != 99999 })
	if _, err := c.acquire(); err != nil {
		t.Fatalf("a dead breaker's lock blocked the claim: %v", err)
	}

	path2 := filepath.Join(dir, "other.claim")
	writeClaim(t, path2, "dead-claim", 99999, time.Now())
	live := fmt.Sprintf("live-breaker %d %d", 4242, time.Now().UnixNano())
	if err := os.WriteFile(path2+".break", []byte(live), 0o600); err != nil {
		t.Fatal(err)
	}
	c2 := testClaim(path2, func(pid int) bool { return pid != 99999 })
	c2.timeout = 100 * time.Millisecond
	if _, err := c2.acquire(); err == nil {
		t.Fatal("broke the claim while a live break lock was held")
	}
	if b, _ := os.ReadFile(path2 + ".break"); string(b) != live {
		t.Fatalf("a live break lock was removed or altered: %q", b)
	}
}

func TestAgedBreakLockIsBroken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.claim")
	writeClaim(t, path, "dead-claim", 99999, time.Now())
	writeClaim(t, path+".break", "old-breaker", 4242, time.Now().Add(-time.Hour))
	c := testClaim(path, func(pid int) bool { return pid != 99999 })
	c.breakAge = time.Minute
	if _, err := c.acquire(); err != nil {
		t.Fatalf("an aged break lock blocked the claim: %v", err)
	}
}

// Many waiters racing over a crashed breaker's lock must never hold the
// break lock together: one waiter breaking it must not delete the fresh lock
// another waiter has since taken.
func TestBreakLockStaysExclusiveWhileBreakingACrashedOne(t *testing.T) {
	for round := 0; round < 20; round++ {
		path := filepath.Join(t.TempDir(), "db.claim")
		writeClaim(t, path+".break", "crashed", 99999, time.Now())
		alive := func(pid int) bool { return pid != 99999 }

		var inside, violations int32
		var wg sync.WaitGroup
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c := testClaim(path, alive)
				for j := 0; j < 10; j++ {
					token, ok := c.lockBreak()
					if !ok {
						t.Error("could not take the break lock")
						return
					}
					if atomic.AddInt32(&inside, 1) != 1 {
						atomic.AddInt32(&violations, 1)
					}
					time.Sleep(100 * time.Microsecond)
					atomic.AddInt32(&inside, -1)
					c.unlockBreak(token)
				}
			}()
		}
		wg.Wait()
		if violations != 0 {
			t.Fatalf("round %d: %d times two waiters held the break lock at once", round, violations)
		}
	}
}

// Many waiters racing to break one stale claim must still serialise.
func TestClaimWaitersNeverBothOwnTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.claim")
	writeClaim(t, path, "dead", 99999, time.Now())
	alive := func(pid int) bool { return pid != 99999 }

	var inside, violations int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := testClaim(path, alive)
			for j := 0; j < 20; j++ {
				nonce, err := c.acquire()
				if err != nil {
					t.Error(err)
					return
				}
				if atomic.AddInt32(&inside, 1) != 1 {
					atomic.AddInt32(&violations, 1)
				}
				time.Sleep(200 * time.Microsecond)
				atomic.AddInt32(&inside, -1)
				if err := c.release(nonce); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if violations != 0 {
		t.Fatalf("%d times two waiters owned the lock at once", violations)
	}
}
