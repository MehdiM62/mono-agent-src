package browserjev

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/jevpick"
	"github.com/monoes/mono-agent/internal/profileinfo"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// chooserDriver is a fakeDriver whose Go button (e3) asks for a file chooser.
type fakeChooser struct {
	*fakeDriver
	multiple bool
	asked    bool
	armed    int
	given    [][]jevpick.ChooserFile
}

func (d *fakeChooser) Act(ctx context.Context, p *jevpick.PageState, a *jevpick.Action, text string) error {
	if a.ID == "e3" {
		d.asked = true
	}
	return d.fakeDriver.Act(ctx, p, a, text)
}

func (d *fakeChooser) ArmFileChooser() error { d.armed++; return nil }

func (d *fakeChooser) WaitFileChooser(context.Context, time.Duration) (*jevpick.Chooser, error) {
	if !d.asked {
		return nil, nil
	}
	d.asked = false
	return &jevpick.Chooser{Multiple: d.multiple}, nil
}

func (d *fakeChooser) SetChooserFiles(f []jevpick.ChooserFile) error {
	d.given = append(d.given, f)
	return nil
}

func writeFiles(t *testing.T, names ...string) []interface{} {
	t.Helper()
	dir := t.TempDir()
	var paths []interface{}
	for _, n := range names {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte("content of "+n), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

func runWithFiles(t *testing.T, drv driver, files any) (map[string]interface{}, error) {
	t.Helper()
	n := &Node{writer: zurich, open: func(context.Context, string) (driver, func(), error) { return drv, func() {}, nil }}
	out, err := n.Execute(testCtx(), workflow.NodeInput{}, map[string]interface{}{
		"url": "https://example.test/", "goal": "Search for zurich", "api_key": "k", "files": files,
	})
	if err != nil {
		return nil, err
	}
	return out[0].Items[0].JSON, nil
}

func TestFilesAnswerTheFileChooserAClickOpens(t *testing.T) {
	policyServer(t, nil)
	drv := &fakeChooser{fakeDriver: &fakeDriver{}}
	res, err := runWithFiles(t, drv, writeFiles(t, "cv.pdf", "photo.png"))
	if err != nil {
		t.Fatal(err)
	}
	if res["status"] != "done" {
		t.Fatalf("status = %v (%v)", res["status"], res["reason"])
	}
	if drv.armed != 1 || len(drv.given) != 1 || len(drv.given[0]) != 1 || drv.given[0][0].Name != "cv.pdf" || drv.given[0][0].Mime != "application/pdf" {
		t.Fatalf("armed=%d given=%+v", drv.armed, drv.given)
	}
	steps := res["steps"].([]step)
	if steps[1].Text != "uploaded cv.pdf" {
		t.Errorf("steps = %+v", steps)
	}
}

func TestAnInputThatTakesSeveralFilesGetsThemAll(t *testing.T) {
	policyServer(t, nil)
	drv := &fakeChooser{fakeDriver: &fakeDriver{}, multiple: true}
	if _, err := runWithFiles(t, drv, writeFiles(t, "a.txt", "b.txt")); err != nil {
		t.Fatal(err)
	}
	if len(drv.given) != 1 || len(drv.given[0]) != 2 {
		t.Fatalf("given = %+v", drv.given)
	}
}

func TestFilesNeedADriverThatCanTakeThem(t *testing.T) {
	policyServer(t, nil)
	if _, err := runWithFiles(t, &fakeDriver{}, writeFiles(t, "cv.pdf")); err == nil || !strings.Contains(err.Error(), "file chooser") {
		t.Fatalf("err = %v", err)
	}
}

func TestFilesConfig(t *testing.T) {
	policyServer(t, nil)
	for name, files := range map[string]any{
		"missing file": []interface{}{filepath.Join(t.TempDir(), "nope.pdf")},
		"not a path":   []interface{}{3},
		"bad JSON":     "[nope",
		"a number":     7,
	} {
		if _, err := runWithFiles(t, &fakeChooser{fakeDriver: &fakeDriver{}}, files); err == nil || !strings.Contains(err.Error(), "files") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// A single path, or the JSON text of a list, is accepted.
	p := writeFiles(t, "cv.pdf")[0].(string)
	for _, files := range []any{p, `["` + p + `"]`, []string{p}} {
		drv := &fakeChooser{fakeDriver: &fakeDriver{}}
		if _, err := runWithFiles(t, drv, files); err != nil || len(drv.given) != 1 {
			t.Errorf("%T: err=%v given=%v", files, err, drv.given)
		}
	}
}

// An AI-started run gives a page a file from this machine only on a local
// page, or at full-no-approvals.
func TestUploadAllowedForAIRuns(t *testing.T) {
	user := &runGate{}
	ai := func(level string) *runGate {
		return &runGate{ai: true, who: caller{name: "an agent"}, pol: browserctl.Policy{Agent: true, Level: level}}
	}
	for _, tt := range []struct {
		g    *runGate
		url  string
		want bool
	}{
		{user, "https://shop.example/", true},
		{ai(browserctl.LevelFull), "https://shop.example/", false},
		{ai(browserctl.LevelDev), "https://shop.example/", false},
		{ai(browserctl.LevelFull), "http://localhost:3000/", true},
		{ai(browserctl.LevelFullNoApprovals), "https://shop.example/", true},
	} {
		if err := tt.g.uploadAllowed(tt.url); (err == nil) != tt.want {
			t.Errorf("%+v on %s: %v", tt.g.pol, tt.url, err)
		}
	}
	files, err := browserctl.ReadUploadFiles([]string{writeFiles(t, "cv.pdf")[0].(string)})
	if err != nil {
		t.Fatal(err)
	}
	drv := &fakeChooser{fakeDriver: &fakeDriver{}, asked: true}
	given, end, err := answerChooser(context.Background(), drv, ai(browserctl.LevelFull), "https://shop.example/", files)
	if err != nil || len(given) != 0 || !strings.Contains(end, "browser upload") || len(drv.given) != 0 {
		t.Fatalf("a gated run gave a file: given=%v end=%q err=%v", given, end, err)
	}
	// Files used up: the run stops rather than leaving the dialog.
	drv.asked = true
	if _, end, err = answerChooser(context.Background(), drv, user, "https://shop.example/", nil); err != nil || !strings.Contains(end, "every file") {
		t.Fatalf("no files left: end=%q err=%v", end, err)
	}
}

func TestProfileInfoBecomesValues(t *testing.T) {
	keyring.MockInit()
	db, err := storage.NewDatabase(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Close() })
	if _, err := db.DB.Exec(`INSERT INTO profiles (id, name, root_dir) VALUES ('p1', 'one', ?)`, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db.DB), "p1")
	for k, v := range map[string]string{"first_name": "Ada", "email": "ada@example.org", "passport_number": "X1234567", "tax_id": "ab"} {
		if profileinfo.Lookup(k) == nil {
			t.Fatalf("catalog has no %s", k)
		}
		if err := profileinfo.Set(ctx, db.DB, "p1", k, v, "user", ""); err != nil {
			t.Fatal(err)
		}
	}
	got, err := withProfileInfo(ctx, []namedValue{{Name: "email", Value: "mine@example.org"}})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]namedValue{}
	for _, v := range got {
		by[v.Name] = v
	}
	if by["first_name"].Value != "Ada" || by["first_name"].Secret {
		t.Errorf("first_name = %+v", by["first_name"])
	}
	if by["email"].Value != "mine@example.org" {
		t.Errorf("the node's own value was replaced: %+v", by["email"])
	}
	if p := by["passport_number"]; p.Value != "X1234567" || !p.Secret {
		t.Errorf("a sensitive field must be a secret: %+v", p)
	}
	if _, ok := by["tax_id"]; ok && by["tax_id"].Secret {
		t.Errorf("a secret too short to keep out of page text must be left out: %+v", by["tax_id"])
	}
	if _, err := withProfileInfo(context.Background(), nil); err == nil {
		t.Error("no database: want an error")
	}
}
