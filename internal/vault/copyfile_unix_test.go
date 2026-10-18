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
