//go:build unix

package checkkeys

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Only a regular file is read: a FIFO, which would block until something
// writes to it, a device such as /dev/zero, which never ends, and a
// directory are each reported at once, and the other files still checked.
func TestCheckReadsOnlyRegularFiles(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "fifo.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan Report, 1)
	go func() {
		rep, _ := Check(nil, []string{fifo, "/dev/zero", t.TempDir(), planted})
		done <- rep
	}()
	select {
	case rep := <-done:
		if len(rep.Errors) != 3 || len(rep.Findings) != 2 || rep.ExitCode() != ExitBadInput {
			t.Fatalf("%+v", rep)
		}
		for _, e := range rep.Errors {
			if !strings.HasSuffix(e, ": not a regular file") {
				t.Errorf("error %q", e)
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the check blocked on the FIFO")
	}
}
