package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/browserctl/browserctltest"
	"github.com/monoes/mono-agent/internal/extension"
)

// applyRig is an apply-kind agent session: its token names task id, an open
// application-apply task of the default profile, with the snapshot and work
// folders next to the database.
type applyRig struct {
	bt             *browserTest
	id             string
	ctx, work, bad string
}

func newApplyRig(t *testing.T, status string) *applyRig {
	t.Helper()
	bt := newBrowserTest(t, true)
	id := aitask.NewID()
	if _, err := (&aitask.Store{DB: bt.db().DB, Profile: "default"}).Insert(aitask.Task{ID: id, Kind: "application-apply",
		ConversationID: "c", Status: status, Goal: "g", Access: aitask.AccessWorkspace, BudgetUSD: 1}); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(filepath.Dir(bt.cfg.DBPath), "aitask", id)
	r := &applyRig{bt: bt, id: id, ctx: filepath.Join(base, "context"), work: filepath.Join(base, "work"), bad: t.TempDir()}
	for _, d := range []string{r.ctx, r.work} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(r.ctx, "document-cv.pdf"), []byte("%PDF cv"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.bad, "id_rsa"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	browserctltest.AsApplyTask(t, id) // after the rig's HOME: the record lives under it
	bt.fake.AddTab("http://localhost:3000/", "Coder · Agent", true)
	bt.fake.On(extension.CmdSetFiles, func(*extension.Command) (any, error) { return map[string]any{"filesSet": true, "count": 1}, nil })
	return r
}

func TestApplyAgentUploadsOnlyItsTasksDocuments(t *testing.T) {
	r := newApplyRig(t, aitask.StatusRunning)
	other := filepath.Join(filepath.Dir(r.ctx), "..", "at-other", "context")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	otherDoc := filepath.Join(other, "document-cv.pdf")
	if err := os.WriteFile(otherDoc, []byte("other"), 0o400); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(r.ctx, "sneaky.pdf")
	if err := os.Symlink(filepath.Join(r.bad, "id_rsa"), link); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{
		"an ssh-style path":         filepath.Join(r.bad, "id_rsa"),
		"another task's snapshot":   otherDoc,
		"a symlink in the snapshot": link,
	} {
		if _, err := r.bt.run("upload", "input[type=file]", p); err == nil || jsonCode(err) != "path_not_allowed" {
			t.Errorf("%s: %v (code %q)", name, err, jsonCode(err))
		}
	}
	if len(setFilesCalls(r.bt)) != 0 {
		t.Fatal("a refused upload reached the page")
	}
	res := r.bt.runJSON("upload", "input[type=file]", filepath.Join(r.ctx, "document-cv.pdf"))
	if res["uploaded"] != float64(1) {
		t.Fatalf("own document: %v", res)
	}
}

func TestApplyAgentScreenshotsOnlyIntoItsWorkFolder(t *testing.T) {
	r := newApplyRig(t, aitask.StatusNeedsUser)
	t.Chdir(r.bad) // an agent whose cwd is somewhere else: the default location is refused too
	for name, args := range map[string][]string{
		"outside, absolute": {"screenshot", "--out", filepath.Join(r.bad, "shot.png")},
		"outside, relative": {"screenshot", "--out", "shot.png"},
		"default location":  {"screenshot"},
		"the snapshot":      {"screenshot", "--out", filepath.Join(r.ctx, "shot.png")},
	} {
		if _, err := r.bt.run(args...); err == nil || jsonCode(err) != "path_not_allowed" {
			t.Errorf("%s: %v (code %q)", name, err, jsonCode(err))
		}
	}
	if _, err := os.Stat(filepath.Join(r.bad, "shot.png")); err == nil {
		t.Fatal("a refused screenshot was written")
	}
	res := r.bt.runJSON("screenshot", "--out", filepath.Join(r.work, "shot.png"))
	if p, _ := res["path"].(string); p == "" {
		t.Fatalf("screenshot in the work folder: %v", res)
	}
	if _, err := os.Stat(filepath.Join(r.work, "shot.png")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(r.work) // the real cwd of the task: the default location works
	if res := r.bt.runJSON("screenshot"); res["path"] == "" {
		t.Fatalf("default screenshot in the work folder: %v", res)
	}
}

// A session whose task is over, unknown or missing may not read or write at all.
func TestApplyAgentWithoutAnOpenTaskIsRefused(t *testing.T) {
	r := newApplyRig(t, aitask.StatusComplete)
	if _, err := r.bt.run("upload", "input[type=file]", filepath.Join(r.ctx, "document-cv.pdf")); err == nil {
		t.Error("upload with a completed task")
	}
	if _, err := r.bt.run("screenshot", "--out", filepath.Join(r.work, "s.png")); err == nil {
		t.Error("screenshot with a completed task")
	}
	browserctltest.AsAgentKind(t, "apply") // a token that names no task
	if _, err := r.bt.run("screenshot", "--out", filepath.Join(r.work, "s.png")); err == nil {
		t.Error("screenshot with a task-less apply token")
	}
}
