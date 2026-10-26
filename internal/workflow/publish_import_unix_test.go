//go:build !windows

package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A workflow file that is a named pipe (or a link) is never opened by the importer, so it cannot
// hold the database's write lock: the import returns at once and writes by others still work.
func TestImportNeverBlocksOnAPipeOrLink(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	dir := h.files.dir
	if err := syscall.Mkfifo(filepath.Join(dir, "wf-pipe.json"), 0o600); err != nil {
		t.Skip("no named pipes here:", err)
	}
	if err := os.Symlink(filepath.Join(dir, "wf-g.json"), filepath.Join(dir, "wf-link.json")); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for _, id := range []string{"wf-pipe", "wf-link"} {
			if res, err := h.ImportFile(ctx, id, nil); err != nil || res.Imported {
				done <- fmt.Errorf("%s: %+v %v", id, res, err)
				return
			}
		}
		_, err := s.db.Exec(`UPDATE workflows SET updated_at = updated_at WHERE id = 'wf-g'`)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the importer is stuck on a special file")
	}
	// Recovery's and the publication's own reads are bounded the same way.
	if _, _, err := h.files.readRaw("wf-pipe"); err == nil {
		t.Fatal("a pipe was read as a workflow file")
	}
}
