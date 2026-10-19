//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestUploadBatchOnlyUploadsIssuedRegularFiles(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.txt")
	forged := filepath.Join(dir, "forged.txt")
	link := filepath.Join(dir, "link.txt")
	fifo := filepath.Join(dir, "fifo.txt")
	for _, p := range []string{good, forged} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(good, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip("mkfifo unavailable:", err)
	}

	a := &App{}
	a.issueDocPaths([]string{good, link, fifo}) // forged was never issued
	var uploaded []string
	res, err := a.uploadBatch([]string{good, forged, link, fifo}, func(_ context.Context, p string) (UploadResult, error) {
		uploaded = append(uploaded, p)
		return UploadResult{Indexed: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(uploaded) != 1 || uploaded[0] != good || res.Uploaded != 1 || len(res.Failed) != 3 {
		t.Fatalf("uploaded=%v res=%+v", uploaded, res)
	}
	if a.docBatchBusy.Load() {
		t.Fatal("busy flag not released")
	}
	// The issued set is consumed: a second batch with the same path is refused.
	res, _ = a.uploadBatch([]string{good}, func(context.Context, string) (UploadResult, error) {
		t.Fatal("uploaded a path from a consumed set")
		return UploadResult{}, nil
	})
	if len(res.Failed) != 1 {
		t.Fatalf("res=%+v", res)
	}
}

func TestUploadBatchReportsATimeoutAndKeepsGoing(t *testing.T) {
	old := uploadFileTimeout
	uploadFileTimeout = 50 * time.Millisecond
	defer func() { uploadFileTimeout = old }()
	dir := t.TempDir()
	slow, fast := filepath.Join(dir, "slow.txt"), filepath.Join(dir, "fast.txt")
	for _, p := range []string{slow, fast} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{}
	a.issueDocPaths([]string{slow, fast})
	res, err := a.uploadBatch([]string{slow, fast}, func(ctx context.Context, p string) (UploadResult, error) {
		if p == slow {
			<-ctx.Done()
			return UploadResult{}, errors.New("signal: killed")
		}
		return UploadResult{Indexed: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 || len(res.Failed) != 1 || !strings.Contains(res.Failed[0], "slow.txt: timed out after") {
		t.Fatalf("res=%+v", res)
	}
	if a.docBatchBusy.Load() {
		t.Fatal("busy flag not released")
	}
}

func TestUploadBatchSuccessAfterDeadlineIsNotAFailure(t *testing.T) {
	old := uploadFileTimeout
	uploadFileTimeout = 20 * time.Millisecond
	defer func() { uploadFileTimeout = old }()
	p := filepath.Join(t.TempDir(), "late.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	a.issueDocPaths([]string{p})
	res, _ := a.uploadBatch([]string{p}, func(ctx context.Context, _ string) (UploadResult, error) {
		<-ctx.Done() // the deadline passes, but the upload still finished
		return UploadResult{Indexed: true}, nil
	})
	if res.Uploaded != 1 || len(res.Failed) != 0 {
		t.Fatalf("res=%+v", res)
	}
}

func TestUploadDocumentArgsPassNoFollow(t *testing.T) {
	args, err := uploadDocumentArgs("p1", "/tmp/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	flag, path := -1, -1
	for i, x := range args {
		switch x {
		case "--no-follow":
			flag = i
		case "/tmp/a.txt":
			path = i
		}
	}
	if flag < 0 || path < 0 || flag > path {
		t.Fatalf("argv = %v", args)
	}
}
