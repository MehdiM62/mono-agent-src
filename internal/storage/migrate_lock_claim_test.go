package storage

import (
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
		path: path, alive: alive, staleAfter: time.Hour,
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
