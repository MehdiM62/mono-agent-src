package fsconfine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrHiddenHomeWrite is wrapped by WritePath's refusal of a hidden file or
// folder in the user's home.
var ErrHiddenHomeWrite = errors.New("a workflow the user has not approved writes no hidden file or folder in their home")

type homeGuardKey struct{}

// GuardHiddenHome marks ctx as a run of a workflow the user never approved
// (internal/workflow): WritePath then refuses hidden files and folders in
// the user's home outside project, the profile's project folder. Such a file
// (a shell's startup file, an autostart or systemd entry, ~/.ssh,
// ~/.monoagent) can run code later, outside every check the run is under.
func GuardHiddenHome(ctx context.Context, project string) context.Context {
	return context.WithValue(ctx, homeGuardKey{}, project)
}

// WritePath is Path for a file a node writes or creates. In a run
// GuardHiddenHome marked, a path in the user's home that goes through a
// hidden file or folder (a name starting with "."; also AppData on Windows
// and Library on macOS, where startup entries live) is refused unless it lies
// in the project folder. Symlinks and a relative path (to the working
// directory) are resolved first, so a link can't lead there unseen.
func WritePath(ctx context.Context, p string) (string, error) {
	r, err := Path(ctx, p)
	if err != nil {
		return "", err
	}
	project, guarded := ctx.Value(homeGuardKey{}).(string)
	if !guarded {
		return r, nil
	}
	abs, err := filepath.Abs(r)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrHiddenHomeWrite, p, err)
	}
	resolved, err := resolveExisting(filepath.Clean(abs))
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrHiddenHomeWrite, p, err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%w: %s: no home folder to check against (%v)", ErrHiddenHomeWrite, p, err)
	}
	if h, err := filepath.EvalSymlinks(home); err == nil {
		home = h
	}
	if !within(home, resolved) {
		return r, nil
	}
	if project != "" {
		if root, err := resolveExisting(filepath.Clean(project)); err == nil && within(root, resolved) {
			return r, nil
		}
	}
	rel, _ := filepath.Rel(home, resolved)
	parts := strings.Split(rel, string(filepath.Separator))
	for i, part := range parts {
		if strings.HasPrefix(part, ".") && part != "." || i == 0 && hiddenTop(part) {
			return "", fmt.Errorf("%w: %s is in %s; save or activate the workflow in the app or your terminal to let it write there, or write inside the project folder %s",
				ErrHiddenHomeWrite, p, filepath.Join(home, filepath.Join(parts[:i+1]...)), project)
		}
	}
	return r, nil
}

// hiddenTop reports a top-level home folder that holds startup entries
// without a leading dot on this OS.
func hiddenTop(name string) bool {
	switch runtime.GOOS {
	case "windows":
		return strings.EqualFold(name, "AppData")
	case "darwin":
		return name == "Library"
	}
	return false
}
