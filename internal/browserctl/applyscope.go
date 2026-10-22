package browserctl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ApplyScope is where an apply-kind agent may touch files (plan section 4b,
// item 11): it uploads only the files of its own task's read-only context
// snapshot, and writes (screenshots) only inside its own task's work folder.
// Paths are canonical: the file, its ancestors and the roots go through
// EvalSymlinks, so a link inside the snapshot that points out, a `..`, or
// another task's folder does not pass.
type ApplyScope struct {
	ContextDir string // the task's snapshot (read)
	WorkDir    string // the task's work folder (write)
}

// CheckUpload refuses any of paths (~ expanded) that is not an existing file
// under the task's context snapshot after resolving links.
func (s *ApplyScope) CheckUpload(paths []string) error {
	root, err := filepath.EvalSymlinks(s.ContextDir)
	if err != nil {
		return fmt.Errorf("this task's context folder is not available: %w", err)
	}
	for _, p := range paths {
		abs, err := uploadPath(p)
		if err != nil {
			return err
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return fmt.Errorf("upload: %w", err)
		}
		if !within(root, real) {
			return &OutOfScopeError{Path: p, What: "an apply task uploads only the documents of its own context"}
		}
	}
	return nil
}

// CheckWrite refuses a write to path (made absolute against the working
// directory) unless it lands inside the task's work folder once the existing
// part of the path is resolved. The file need not exist yet.
func (s *ApplyScope) CheckWrite(path string) error {
	root, err := filepath.EvalSymlinks(s.WorkDir)
	if err != nil {
		return fmt.Errorf("this task's work folder is not available: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	resolved, err := resolveExisting(abs)
	if err != nil {
		return err
	}
	if !within(root, resolved) {
		return &OutOfScopeError{Path: path, What: "an apply task writes only inside its own work folder"}
	}
	return nil
}

// OutOfScopeError: a file the apply task may not read or write.
type OutOfScopeError struct{ Path, What string }

func (e *OutOfScopeError) Error() string {
	return fmt.Sprintf("%s is outside what this task may use: %s", e.Path, e.What)
}

// resolveExisting resolves the longest existing prefix of abs through links and
// appends the rest, so a symlinked ancestor of a new file is followed.
func resolveExisting(abs string) (string, error) {
	rest := ""
	p := abs
	for {
		real, err := filepath.EvalSymlinks(p)
		if err == nil {
			return filepath.Join(real, rest), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", err
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
