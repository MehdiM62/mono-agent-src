package browserctl_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/browserctl/browserctltest"
)

type scopeRig struct {
	scope              *browserctl.ApplyScope
	ctx, work, outside string
	otherTask          string
}

func newScopeRig(t *testing.T) *scopeRig {
	t.Helper()
	base := t.TempDir()
	r := &scopeRig{
		ctx:       filepath.Join(base, "aitask", "at-1", "context"),
		work:      filepath.Join(base, "aitask", "at-1", "work"),
		otherTask: filepath.Join(base, "aitask", "at-2", "context"),
		outside:   filepath.Join(base, "home", ".ssh"),
	}
	for _, d := range []string{r.ctx, r.work, r.otherTask, r.outside} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{filepath.Join(r.ctx, "document-cv.pdf"), filepath.Join(r.otherTask, "document-cv.pdf"), filepath.Join(r.outside, "id_rsa")} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r.scope = &browserctl.ApplyScope{ContextDir: r.ctx, WorkDir: r.work}
	return r
}

func TestApplyScopeUpload(t *testing.T) {
	r := newScopeRig(t)
	if err := r.scope.CheckUpload([]string{filepath.Join(r.ctx, "document-cv.pdf")}); err != nil {
		t.Fatalf("the task's own document: %v", err)
	}
	link := filepath.Join(r.ctx, "link")
	if err := os.Symlink(filepath.Join(r.outside, "id_rsa"), link); err != nil {
		t.Fatal(err)
	}
	dirLink := filepath.Join(r.ctx, "dirlink")
	if err := os.Symlink(r.outside, dirLink); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{
		"an ssh-style path":          filepath.Join(r.outside, "id_rsa"),
		"another task's snapshot":    filepath.Join(r.otherTask, "document-cv.pdf"),
		"a symlink in the snapshot":  link,
		"via a symlinked folder":     filepath.Join(dirLink, "id_rsa"),
		"traversal out of it":        filepath.Join(r.ctx, "..", "..", "at-2", "context", "document-cv.pdf"),
		"the work folder":            r.work,
		"a file that does not exist": filepath.Join(r.ctx, "nope.pdf"),
	} {
		err := r.scope.CheckUpload([]string{p})
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// One bad path in several refuses all of them.
	if err := r.scope.CheckUpload([]string{filepath.Join(r.ctx, "document-cv.pdf"), filepath.Join(r.outside, "id_rsa")}); err == nil {
		t.Error("a list with one outside path was accepted")
	}
}

func TestApplyScopeWrite(t *testing.T) {
	r := newScopeRig(t)
	for _, p := range []string{filepath.Join(r.work, "shot.png"), filepath.Join(r.work, ".monoagent", "screenshots", "a.png"), r.work + "/./x/../y.png"} {
		if err := r.scope.CheckWrite(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	dirLink := filepath.Join(r.work, "out")
	if err := os.Symlink(r.outside, dirLink); err != nil {
		t.Fatal(err)
	}
	fileLink := filepath.Join(r.work, "fl.png")
	if err := os.Symlink(filepath.Join(r.outside, "id_rsa"), fileLink); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{
		"outside the work folder":        filepath.Join(r.outside, "shot.png"),
		"the snapshot":                   filepath.Join(r.ctx, "shot.png"),
		"another task's folder":          filepath.Join(r.otherTask, "..", "work", "shot.png"),
		"via a symlinked folder":         filepath.Join(dirLink, "shot.png"),
		"onto a symlink out":             fileLink,
		"traversal":                      filepath.Join(r.work, "..", "..", "at-2", "x.png"),
		"a new folder past a link":       filepath.Join(dirLink, "new", "shot.png"),
		"a sibling with the same prefix": r.work + "-evil/shot.png",
	} {
		if err := r.scope.CheckWrite(p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestApplyTaskIDComesFromTheVouchedToken(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	browserctltest.AsApplyTask(t, "at-0123456789abcdef")
	if got := browserctl.ApplyTaskID(); got != "at-0123456789abcdef" {
		t.Fatalf("task = %q", got)
	}
	// An apply token without a task, a coder token, and no token carry none.
	t.Setenv("MONOAGENT_APPLY_TASK", "at-ffffffffffffffff") // no such variable counts
	if got := browserctl.ApplyTaskID(); got != "at-0123456789abcdef" {
		t.Fatalf("task changed by the environment: %q", got)
	}
	browserctltest.AsAgentKind(t, browserctl.KindApply)
	if got := browserctl.ApplyTaskID(); got != "" {
		t.Fatalf("a task-less apply token names %q", got)
	}
	browserctltest.AsAgentKind(t, browserctl.KindCoder)
	if got := browserctl.ApplyTaskID(); got != "" {
		t.Fatalf("a coder token names %q", got)
	}
	_ = strings.TrimSpace
}
