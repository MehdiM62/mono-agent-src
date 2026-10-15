package storage

import (
	"errors"
	"io/fs"
	"os"

	"github.com/monoes/mono-agent/internal/statedir"
)

// isNewFile reports whether path does not exist yet.
func isNewFile(path string) bool {
	_, err := os.Stat(path)
	return errors.Is(err, fs.ErrNotExist)
}

// privatizeNewDB makes a database file this process just created readable
// by its owner only. SQLite creates the -wal and -shm files with the
// database file's mode, so they follow. An existing database keeps its mode
// (opening one must not change it; `doctor --fix` tightens old installs).
func privatizeNewDB(created bool, path string) {
	if created {
		_ = statedir.Tighten(path)
	}
}
