//go:build !windows

package vault

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestCopyFileRefusesAFIFOWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip("mkfifo unavailable:", err)
	}
	done := make(chan error, 1)
	go func() { done <- copyFile(fifo, filepath.Join(dir, "out")) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("copied a FIFO")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("copyFile blocked on a FIFO")
	}
	if _, err := os.Stat(filepath.Join(dir, "out")); err == nil {
		t.Fatal("created the destination for a FIFO")
	}
}

func TestCopyFileOptNoFollowRefusesASymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(real, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if err := copyFileOpt(link, filepath.Join(dir, "refused"), true); err == nil {
		t.Fatal("copied a symlink with noFollow")
	}
	if _, err := os.Stat(filepath.Join(dir, "refused")); err == nil {
		t.Fatal("created the destination for a refused symlink")
	}
	if err := copyFileOpt(link, filepath.Join(dir, "followed"), false); err != nil {
		t.Fatalf("symlink should still copy by default: %v", err)
	}
	if err := copyFileOpt(real, filepath.Join(dir, "plain"), true); err != nil {
		t.Fatalf("regular file with noFollow: %v", err)
	}
}
