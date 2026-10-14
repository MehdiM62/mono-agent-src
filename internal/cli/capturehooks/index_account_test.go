package capturehooks

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/account/accounttest"
)

// A bridge started while the account is locked does not run its catch-up index sweep: the sweep
// waits, and runs once a login is valid.
func TestTheIndexSweepWaitsForALogin(t *testing.T) {
	signedIn := accounttest.Install(t, accounttest.SignedIn)
	account.InstallForTest(t, nil) // strict, the date behind the clock, no guard: locked
	prev := sweepAccountPoll
	sweepAccountPoll = 10 * time.Millisecond
	t.Cleanup(func() { sweepAccountPoll = prev })

	var opened atomic.Int32
	open := func() (*sql.DB, func(), error) { opened.Add(1); return noDB() }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sweepCaptureIndexAfter(ctx, nil, open, time.Millisecond)

	time.Sleep(200 * time.Millisecond)
	if n := opened.Load(); n != 0 {
		t.Fatalf("a locked bridge opened the database %d times for the index sweep", n)
	}
	account.InstallForTest(t, signedIn)
	for deadline := time.Now().Add(5 * time.Second); opened.Load() == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the sweep did not run after the login")
		}
	}
}
