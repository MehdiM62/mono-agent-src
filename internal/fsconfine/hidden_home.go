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

// ErrUnapprovedPath is wrapped by the refusals GuardHiddenHome adds to Path
// and WritePath.
var ErrUnapprovedPath = errors.New("a workflow the user has not approved can't use this path")

type homeGuardKey struct{}

// GuardHiddenHome marks ctx as a run of a workflow the user never approved
// (internal/workflow). Its file steps then neither read nor write a hidden
// file or folder of the user's home outside project, the profile's project
// folder: there live keys and tokens (~/.ssh, ~/.monoagent) and files that
// run code later (a shell's startup file, an autostart or systemd entry).
// They also write nothing into a folder of the home on PATH (~/bin), nor git
// hooks or a git config, which run at the next commit, in the project too.
func GuardHiddenHome(ctx context.Context, project string) context.Context {
	return context.WithValue(ctx, homeGuardKey{}, project)
}

// WritePath is Path for a file a node writes or creates: in a run
// GuardHiddenHome marked it also refuses git hooks and git configs anywhere
// and the folders of the home on PATH.
func WritePath(ctx context.Context, p string) (string, error) {
	r, err := confine(ctx, p)
	if err != nil {
		return "", err
	}
	if err := homeRule(ctx, p, r, true); err != nil {
		return "", err
	}
	return r, nil
}

// homeRule applies GuardHiddenHome's rule to p, which confine resolved to r.
// Symlinks and a relative path (to the working directory) are resolved first,
// so a link can't lead past it.
func homeRule(ctx context.Context, p, r string, write bool) error {
	project, guarded := ctx.Value(homeGuardKey{}).(string)
	if !guarded {
		return nil
	}
	abs, err := filepath.Abs(r)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUnapprovedPath, p, err)
	}
	resolved, err := resolveExisting(filepath.Clean(abs))
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUnapprovedPath, p, err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("%w: %s: no home folder to check against (%v)", ErrUnapprovedPath, p, err)
	}
	if h, err := filepath.EvalSymlinks(home); err == nil {
		home = h
	}
	approve := fmt.Sprintf("; save or activate the workflow in the app or your terminal to let it, or use the project folder %s", project)
	if write && gitRunsIt(resolved) {
		return fmt.Errorf("%w: %s is a git hook or git config, which git runs at the next commit%s", ErrUnapprovedPath, p, approve)
	}
	if project != "" {
		if root, err := resolveExisting(filepath.Clean(project)); err == nil && within(root, resolved) {
			return nil
		}
	}
	if !within(home, resolved) {
		return nil
	}
	rel, _ := filepath.Rel(home, resolved)
	parts := strings.Split(rel, string(filepath.Separator))
	for i, part := range parts {
		if strings.HasPrefix(part, ".") && part != "." || i == 0 && hiddenTop(part) {
			verb := "reads"
			if write {
				verb = "writes"
			}
			return fmt.Errorf("%w: it %s no hidden file or folder in your home, and %s is in %s%s",
				ErrUnapprovedPath, verb, p, filepath.Join(home, filepath.Join(parts[:i+1]...)), approve)
		}
	}
	if write && onPath(home, filepath.Dir(resolved)) {
		return fmt.Errorf("%w: %s is in %s, a folder on PATH, where a file runs as a command%s", ErrUnapprovedPath, p, filepath.Dir(resolved), approve)
	}
	return nil
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

// gitRunsIt reports a path git runs or reads its commands from: a file in a
// .git/hooks folder, or a .git/config.
func gitRunsIt(p string) bool {
	parts := strings.Split(p, string(filepath.Separator))
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == ".git" && (parts[i+1] == "hooks" || parts[i+1] == "config" && i+2 == len(parts)) {
			return true
		}
	}
	return false
}

// onPath reports whether dir, a folder of the home, is on PATH: this
// process's, plus ~/bin and ~/go/bin, which a login shell often adds.
func onPath(home, dir string) bool {
	entries := append(filepath.SplitList(os.Getenv("PATH")), filepath.Join(home, "bin"), filepath.Join(home, "go", "bin"))
	for _, e := range entries {
		if e == "" || !filepath.IsAbs(e) {
			continue
		}
		if r, err := filepath.EvalSymlinks(e); err == nil {
			e = r
		}
		if within(home, e) && filepath.Clean(e) == dir {
			return true
		}
	}
	return false
}
