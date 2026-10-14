package main

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/account/accounttest"
	"github.com/monoes/mono-agent/internal/storage"
)

// busyOrLocked: a locked account is busy, with a reason that names it; every
// other state (dormant included) leaves the usual check, which an empty
// database passes.
func TestAutoRevalidationWaitsWhileTheAccountIsLocked(t *testing.T) {
	db, err := storage.NewDatabase(t.TempDir() + "/auto.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		mode accounttest.Mode
		busy bool
	}{"locked": {accounttest.LockedNoLogin, true}, "signed in": {accounttest.SignedIn, false}, "dormant": {accounttest.Dormant, false}} {
		accounttest.Install(t, c.mode)
		busy, why := busyOrLocked(context.Background(), db.DB)
		if busy != c.busy || (busy && !strings.Contains(why, "monoes.me account is locked")) {
			t.Errorf("%s: busy = %v (%q), want %v", name, busy, why, c.busy)
		}
	}
}
