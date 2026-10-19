package monomind

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func shortSettle(t *testing.T, finish, start time.Duration) {
	t.Helper()
	oldFinish, oldStart := settleFinish, settleStart
	settleFinish, settleStart = finish, start
	t.Cleanup(func() { settleFinish, settleStart = oldFinish, oldStart })
}

// Settle returns as soon as the syncs finish, and leaves a started one that
// is still running once the finish window has passed.
func TestSettleWaitsForFinishThenOnlyForStarts(t *testing.T) {
	shortSettle(t, 200*time.Millisecond, 2*time.Second)
	var quick SyncGroup
	quick.Go(func(func()) { time.Sleep(50 * time.Millisecond) })
	if !quick.Settle() {
		t.Error("Settle reported a quick sync unfinished")
	}

	var slow SyncGroup
	block := make(chan struct{})
	defer close(block)
	slow.Go(func(started func()) {
		started()
		<-block
	})
	begin := time.Now()
	if slow.Settle() {
		t.Fatal("Settle reported a blocked sync finished")
	}
	if waited := time.Since(begin); waited < 200*time.Millisecond || waited > time.Second {
		t.Errorf("Settle waited %s for a started sync, want the 200ms finish window", waited)
	}
}

// A sync not started yet is waited for until it starts, but not past the
// start bound.
func TestSettleWaitsForAStartUpToItsBound(t *testing.T) {
	shortSettle(t, 50*time.Millisecond, 500*time.Millisecond)
	block := make(chan struct{})
	defer close(block)

	var late SyncGroup
	late.Go(func(started func()) {
		time.Sleep(200 * time.Millisecond)
		started()
		<-block
	})
	begin := time.Now()
	late.Settle()
	if waited := time.Since(begin); waited < 200*time.Millisecond || waited >= 500*time.Millisecond {
		t.Errorf("Settle waited %s for a sync that starts at 200ms", waited)
	}

	var never SyncGroup
	never.Go(func(func()) { <-block })
	begin = time.Now()
	never.Settle()
	if waited := time.Since(begin); waited < 500*time.Millisecond || waited > 2*time.Second {
		t.Errorf("Settle waited %s for a sync that never starts, want the 500ms bound", waited)
	}
}

// started is called while the monomind process runs, before it exits.
func TestSyncToKnowledgeGraphStartedReportsTheRunningProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake monomind is a shell script")
	}
	ResetCapabilityCache()
	t.Cleanup(ResetCapabilityCache)
	dir := t.TempDir()
	release := filepath.Join(dir, "release")
	bin := filepath.Join(dir, "monomind")
	script := "#!/bin/sh\ni=0\nwhile [ ! -e '" + release + "' ] && [ $i -lt 300 ]; do sleep 0.05; i=$((i+1)); done\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvOverride, bin)
	done := make(chan error, 1)
	startedCh := make(chan struct{})
	go func() {
		done <- SyncToKnowledgeGraphStarted(t.Context(), nil, "p1", []KGNode{{Name: "n", Type: "t"}}, nil, "test:1", func() { close(startedCh) })
	}()
	select {
	case <-startedCh:
	case err := <-done:
		t.Fatalf("the sync ended (%v) without reporting its process started", err)
	case <-time.After(10 * time.Second):
		t.Fatal("started was not called")
	}
	select {
	case err := <-done:
		t.Fatalf("the sync ended (%v) while its process was still waiting", err)
	default:
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("sync: %v", err)
	}
}
