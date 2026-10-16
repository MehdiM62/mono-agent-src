package fsconfine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func guardHome(t *testing.T) (base, home string) {
	t.Helper()
	base = t.TempDir()
	home = filepath.Join(base, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return base, home
}

// A refusal says how to approve the workflow, and names a hidden file of the
// home as one, not as lying in itself.
func TestARefusalSaysHowToApprove(t *testing.T) {
	_, home := guardHome(t)
	ctx := GuardHiddenHome(context.Background(), Guard{Approve: "run `monoagentcli --profile p workflow activate wf-1`"})
	_, err := WritePath(ctx, filepath.Join(home, ".bashrc"))
	if !errors.Is(err, ErrUnapprovedPath) || !strings.Contains(err.Error(), "workflow activate wf-1") ||
		!strings.Contains(err.Error(), ".bashrc is one") {
		t.Fatalf("err = %v", err)
	}
}

// MonoAgent's own folders the guard names stay usable: the attachments to
// read, the export folder to read and write.
func TestTheGuardsOwnFoldersStayOpen(t *testing.T) {
	_, home := guardHome(t)
	att, out := filepath.Join(home, ".monoagent", "attachments"), filepath.Join(home, ".monoagent", "output")
	ctx := GuardHiddenHome(context.Background(), Guard{ReadOK: []string{att}, WriteOK: []string{out}})
	if _, err := Path(ctx, filepath.Join(att, "m1", "invoice.pdf")); err != nil {
		t.Errorf("read an attachment: %v", err)
	}
	if _, err := WritePath(ctx, filepath.Join(att, "m1", "x.sh")); !errors.Is(err, ErrUnapprovedPath) {
		t.Errorf("write into the attachments: %v", err)
	}
	for _, f := range []func(context.Context, string) (string, error){Path, WritePath} {
		if _, err := f(ctx, filepath.Join(out, "export.json")); err != nil {
			t.Errorf("the export folder: %v", err)
		}
	}
	if _, err := Path(ctx, filepath.Join(home, ".monoagent", "monoagent.db")); !errors.Is(err, ErrUnapprovedPath) {
		t.Errorf("the database next to them: %v", err)
	}
}

// A project folder that is the home or holds it, or lies in a hidden folder
// of the home other than ~/.monoagent/profiles, is no project folder: the
// hidden files stay closed.
func TestAProjectFolderCantOpenTheHome(t *testing.T) {
	base, home := guardHome(t)
	ssh := filepath.Join(home, ".ssh")
	link := filepath.Join(home, "proj-link")
	for _, d := range []string{ssh, filepath.Join(home, "code", "proj"), filepath.Join(home, ".monoagent", "profiles", "p1")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(ssh, link); err != nil {
		t.Fatal(err)
	}
	for _, project := range []string{home, base, ssh, link, filepath.Join(home, ".config", "x")} {
		ctx := GuardHiddenHome(context.Background(), Guard{Project: project})
		if _, err := Path(ctx, filepath.Join(ssh, "id_ed25519")); !errors.Is(err, ErrUnapprovedPath) {
			t.Errorf("project %s opened ~/.ssh: %v", project, err)
		}
	}
	for _, project := range []string{filepath.Join(home, "code", "proj"), filepath.Join(home, ".monoagent", "profiles", "p1")} {
		ctx := GuardHiddenHome(context.Background(), Guard{Project: project})
		if _, err := WritePath(ctx, filepath.Join(project, ".claude", "settings.json")); err != nil {
			t.Errorf("project %s: %v", project, err)
		}
	}
}

// Git hooks and configs, in every place git keeps them, in any case.
func TestGitRunsIt(t *testing.T) {
	sep := string(filepath.Separator)
	j := func(s string) string { return strings.ReplaceAll(s, "/", sep) }
	for _, p := range []string{
		"/p/.git/hooks/pre-commit", "/p/.git/config", "/p/.git/config.worktree",
		"/p/.git/modules/m/hooks/post-checkout", "/p/.git/modules/m/config",
		"/p/.git/worktrees/w/config.worktree", "/srv/repo.git/hooks/update",
		"/p/.GIT/hooks/x", "/p/.git/HOOKS/x",
	} {
		if !gitRunsIt(j(p)) {
			t.Errorf("gitRunsIt(%s) = false", p)
		}
	}
	for _, p := range []string{"/p/.gitignore", "/p/.git/HEAD", "/p/.git/hooks", "/p/src/config.go", "/p/notes.git"} {
		if gitRunsIt(j(p)) {
			t.Errorf("gitRunsIt(%s) = true", p)
		}
	}
}

// The folders on PATH a package manager or a login shell adds are covered,
// in the home and out of it.
func TestOnPathCoversTheUsualFolders(t *testing.T) {
	_, home := guardHome(t)
	t.Setenv("PATH", "") // the built-in folders alone
	for _, d := range []string{filepath.Join(home, "bin"), filepath.Join(home, ".local", "bin"), filepath.Join(home, "go", "bin")} {
		if !onPath(home, d) {
			t.Errorf("onPath(%s) = false", d)
		}
	}
	if runtime.GOOS != "windows" && !onPath(home, "/usr/local/bin") {
		t.Error("onPath(/usr/local/bin) = false")
	}
	if onPath(home, filepath.Join(home, "notes")) {
		t.Error("onPath(~/notes) = true")
	}
}

// A process's environment, where the daemon's keys can be, stays unread.
func TestNoProcessEnvironment(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc is Linux's")
	}
	guardHome(t)
	ctx := GuardHiddenHome(context.Background(), Guard{})
	for _, p := range []string{"/proc/self/environ", "/proc/1/environ"} {
		if _, err := Path(ctx, p); !errors.Is(err, ErrUnapprovedPath) {
			t.Errorf("read %s: %v", p, err)
		}
	}
	if _, err := Path(context.Background(), "/proc/self/environ"); err != nil {
		t.Errorf("an approved run: %v", err)
	}
}

// On a file system that compares names without case (Windows, macOS), a
// path that differs from the home or the project in case is still in them.
func TestNamesCompareWithoutCaseWhereTheFileSystemDoes(t *testing.T) {
	old := caseFolds
	t.Cleanup(func() { caseFolds = old })
	caseFolds = true
	if !in("/Users/Ada", "/users/ada/.SSH/id") || !in("/x/Proj", "/x/proj/f") {
		t.Error("in compares with case")
	}
	caseFolds = false
	if in("/Users/Ada", "/users/ada/.ssh/id") {
		t.Error("in folds case where the file system doesn't")
	}
}
