package aitask

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SnapshotDir is where task taskID's verified context copy lives:
// <stateDir>/aitask/<taskID>/context, read-only (files 0400, folders 0500).
func SnapshotDir(stateDir, taskID string) string {
	return filepath.Join(stateDir, "aitask", taskID, "context")
}

// SnapshotContext copies task taskID's context folder into SnapshotDir and
// checks the COPY against the hash stored at submit (context_changed when it
// differs, whatever the original holds now). The turn is handed the returned
// folder, not the original path, so a later change to the original cannot
// change what the run reads. Anything in the original that is not a regular
// file (a link, a device) is a change. It replaces a snapshot an earlier,
// refused start left behind.
func (s *Service) SnapshotContext(taskID string) (dir string, err error) {
	t, err := s.Store.Get(taskID)
	if err != nil {
		return "", err
	}
	if s.StateDir == "" {
		return "", errf(CodeInvalidInput, "no state folder to snapshot task %s into", taskID)
	}
	dir = SnapshotDir(s.StateDir, taskID)
	if err := removeSnapshot(dir); err != nil {
		return "", err
	}
	if err := copyContext(filepath.Join(t.Cwd, filepath.FromSlash(ContextDir), taskID), dir); err != nil {
		_ = removeSnapshot(dir)
		return "", err
	}
	h, err := HashDir(dir)
	if err != nil {
		_ = removeSnapshot(dir)
		return "", err
	}
	if h != t.ContextHash {
		_ = removeSnapshot(dir)
		return "", errf(CodeContextChanged, "the context folder of task %s changed since it was queued: cancel the task and submit it again", taskID)
	}
	if err := lockSnapshot(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// copyContext copies src (absent is empty) to dst, regular files only.
func copyContext(src, dst string) error {
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	changed := func(what string) error {
		return errf(CodeContextChanged, "the context folder changed since it was queued (%s): cancel the task and submit it again", what)
	}
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == src && errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipAll
			}
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o700)
		case d.Type().IsRegular():
			b, err := readRegular(p, MaxContextFileBytes+4096)
			if err != nil {
				return changed(rel + ": " + err.Error())
			}
			return os.WriteFile(target, b, 0o600)
		}
		return changed(rel + " is not a regular file")
	})
	return err
}

// lockSnapshot makes dir read-only: files 0400, folders 0500.
func lockSnapshot(dir string) error {
	var dirs []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, p)
			return nil
		}
		return os.Chmod(p, 0o400)
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- { // children first
		if err := os.Chmod(dirs[i], 0o500); err != nil {
			return err
		}
	}
	return nil
}

// removeSnapshot deletes a snapshot (and the task folder holding only it).
func removeSnapshot(dir string) error {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
	if err := os.RemoveAll(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// useSnapshot points the prompt's context folder at the snapshot.
func useSnapshot(prompt, taskID, snapshot string) string {
	return strings.ReplaceAll(prompt, ContextDir+"/"+taskID+"/", snapshot+string(filepath.Separator))
}
