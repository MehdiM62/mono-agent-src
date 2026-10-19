package storage

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/monomind"
)

// Close waits for the knowledge-graph syncs still running: they read the
// database it is about to close.
func TestCloseWaitsForRunningKGSyncs(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "kg.db"))
	if err != nil {
		t.Fatal(err)
	}
	release, finished := make(chan struct{}), make(chan struct{})
	db.kg.start(func(func()) {
		<-release
		close(finished)
	})
	closed := make(chan struct{})
	go func() {
		db.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned while a sync was still running")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-closed
	select {
	case <-finished:
	default:
		t.Fatal("Close returned before the sync finished")
	}
}

// A sync stuck in monomind holds Close for about a second, not for the sync:
// the monomind process finishes on its own after this one exits. Nothing
// starts once closing has begun.
func TestKGSyncStopIsBoundedAndFinal(t *testing.T) {
	var k kgSyncs
	block := make(chan struct{})
	defer close(block)
	k.start(func(started func()) {
		started()
		<-block
	})
	begin := time.Now()
	if k.stop() {
		t.Fatal("stop reported every sync done while one was blocked")
	}
	if waited := time.Since(begin); waited < 900*time.Millisecond || waited > 3*time.Second {
		t.Fatalf("stop waited %s for a started sync, want about 1s", waited)
	}
	ran := make(chan struct{}, 1)
	k.start(func(func()) { ran <- struct{}{} })
	select {
	case <-ran:
		t.Fatal("a sync started after closing began")
	case <-time.After(50 * time.Millisecond):
	}
}

// A sync that has not started its monomind process yet is waited for until
// it has, past the second: nothing would send its nodes again.
func TestCloseWaitsForASyncToStart(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "kg.db"))
	if err != nil {
		t.Fatal(err)
	}
	startedAt := make(chan time.Time, 1)
	release := make(chan struct{})
	defer close(release)
	db.kg.start(func(started func()) {
		time.Sleep(1500 * time.Millisecond) // finding and starting monomind on a slow machine
		startedAt <- time.Now()
		started()
		<-release
	})
	begin := time.Now()
	db.Close()
	returned := time.Now()
	select {
	case at := <-startedAt:
		if returned.Before(at) {
			t.Fatal("Close returned before the sync started")
		}
	default:
		t.Fatalf("Close returned after %s, before the sync started", returned.Sub(begin))
	}
	if waited := returned.Sub(begin); waited > 5500*time.Millisecond {
		t.Fatalf("Close waited %s, past its 5s bound", waited)
	}
}

// End to end, with a monomind that takes its time: saving a person and
// closing returns in about a second (a command that saves one used to take
// 5s), and the sync's monomind still ran.
func TestCloseIsBoundedForASlowMonomind(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake monomind is a shell script")
	}
	monomind.ResetCapabilityCache()
	t.Cleanup(monomind.ResetCapabilityCache)
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	ran, release := filepath.Join(dir, "ran"), filepath.Join(dir, "release")
	script := "#!/bin/sh\necho \"$*\" >> '" + ran + "'\n" +
		"i=0\nwhile [ ! -e '" + release + "' ] && [ $i -lt 300 ]; do sleep 0.1; i=$((i+1)); done\n"
	bin := filepath.Join(dir, "bin", "monomind")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(monomind.EnvOverride, bin)
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0o600) })

	db := newUpsertPersonTestDB(t)
	if err := db.UpsertPerson(&Person{PlatformUsername: "ada@example.com", Platform: "email", ProfileID: "default", FullName: "Ada"}); err != nil {
		t.Fatal(err)
	}
	begin := time.Now()
	db.Close()
	if waited := time.Since(begin); waited > 3*time.Second {
		t.Fatalf("Close waited %s for a sync stuck in monomind, want about 1s", waited)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ran); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the sync's monomind never ran")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
