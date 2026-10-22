package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/profiledir"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/testdb"
	"github.com/monoes/mono-agent/internal/vault"
)

// addJob adds a job through the command and returns its id.
func addJob(t *testing.T, dbPath string) string {
	t.Helper()
	out, err := run(t, dbPath, true, "add", "--kind", "job", "--title", "Backend Engineer", "--company", "Acme", "--url", "https://acme.example/1")
	if err != nil {
		t.Fatalf("add: %v (%s)", err, out)
	}
	var r struct{ ID string }
	if err := json.Unmarshal([]byte(out), &r); err != nil || r.ID == "" {
		t.Fatalf("add output %q: %v", out, err)
	}
	return r.ID
}

func withDB(t *testing.T, dbPath string, f func(db *sql.DB)) {
	t.Helper()
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	f(db.DB)
}

func TestShowAssetsContractShape(t *testing.T) {
	dbPath := homeDB(t)
	id := addJob(t, dbPath)

	out, err := run(t, dbPath, true, "show", id, "--assets")
	if err != nil {
		t.Fatalf("show --assets: %v (%s)", err, out)
	}
	var got struct {
		Application json.RawMessage
		Assets      map[string]json.RawMessage
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"posting", "documents", "fill", "people", "messages", "tasks", "task", "links"} {
		if _, ok := got.Assets[k]; !ok {
			t.Errorf("assets lacks %q: %s", k, out)
		}
	}
	if string(got.Assets["task"]) != "null" {
		t.Errorf("task = %s, want null while no TaskLookup is wired", got.Assets["task"])
	}
	if !strings.Contains(string(got.Assets["fill"]), `"status": "none"`) {
		t.Errorf("fill = %s", got.Assets["fill"])
	}
	if !strings.Contains(string(got.Assets["posting"]), "https://acme.example/1") {
		t.Errorf("posting = %s", got.Assets["posting"])
	}
	if len(got.Application) == 0 {
		t.Error("application missing")
	}
}

func TestShowAssetsGathersLinkedThings(t *testing.T) {
	dbPath := homeDB(t)
	id := addJob(t, dbPath)
	root := profiledir.Root(nil, "default")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	cvSrc := filepath.Join(t.TempDir(), "My CV.pdf")
	os.WriteFile(cvSrc, []byte("%PDF cv"), 0o600)
	capSrc := filepath.Join(t.TempDir(), "posting.md")
	os.WriteFile(capSrc, []byte("We are hiring a backend engineer."), 0o600)

	var cvID, capID string
	withDB(t, dbPath, func(db *sql.DB) {
		ctx := vault.ContextWithProfileID(context.Background(), "default")
		var err error
		if cvID, err = vault.RegisterDocument(ctx, db, cvSrc, "upload", id); err != nil {
			t.Fatal(err)
		}
		if capID, err = vault.RegisterDocument(ctx, db, capSrc, "extension"); err != nil {
			t.Fatal(err)
		}
		for _, q := range []string{
			`INSERT INTO people (id, platform_username, platform, full_name, job_title, profile_id) VALUES ('p1', 'jo@acme.example', 'email', 'Jo Recruiter', 'Recruiter', 'default')`,
			`INSERT INTO person_messages (id, person_id, source, direction, subject, body, sent_at, profile_id) VALUES ('m1', 'p1', 'outlook', 'outbound', 'Hello', 'Hi   there,
 I applied.', '2026-10-01T10:00:00Z', 'default')`,
			`INSERT INTO tasks (profile_id, title, status, position, created_at, updated_at) VALUES ('default', 'Follow up', 'ready', 1, 'x', 'x')`,
		} {
			if _, err := db.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	})
	for _, l := range [][2]string{{"person", "p1"}, {"message", "m1"}, {"task", "1"}, {"capture", capID}, {"document", cvID}} {
		if out, err := run(t, dbPath, true, "link", "add", id, "--kind", l[0], "--ref", l[1]); err != nil {
			t.Fatalf("link add %v: %v (%s)", l, err, out)
		}
	}

	out, err := run(t, dbPath, true, "show", id, "--assets")
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Assets Assets }
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	a := got.Assets
	if a.Posting.CapturedText != "We are hiring a backend engineer." || a.Posting.CapturedAt == "" {
		t.Errorf("posting = %+v", a.Posting)
	}
	if len(a.Documents) != 1 || a.Documents[0].Kind != "cv" || a.Documents[0].Source != "upload" ||
		a.Documents[0].Format != "pdf" || !a.Documents[0].Attached {
		t.Errorf("documents = %+v", a.Documents)
	}
	if len(a.People) != 1 || a.People[0].Name != "Jo Recruiter" || a.People[0].Email != "jo@acme.example" {
		t.Errorf("people = %+v", a.People)
	}
	if len(a.Messages) != 1 || a.Messages[0].Direction != "out" || a.Messages[0].Snippet != "Hi there, I applied." {
		t.Errorf("messages = %+v", a.Messages)
	}
	if len(a.Tasks) != 1 || a.Tasks[0].Title != "Follow up" || len(a.Links) != 5 {
		t.Errorf("tasks = %+v links = %+v", a.Tasks, a.Links)
	}

	if out, err := run(t, dbPath, true, "link", "remove", id, "--kind", "person", "--ref", "p1"); err != nil || !strings.Contains(out, `"removed": true`) {
		t.Fatalf("link remove: %v %s", err, out)
	}
	if out, _ := run(t, dbPath, true, "link", "list", id, "--kind", "person"); strings.Contains(out, "p1") {
		t.Fatalf("link list after remove: %s", out)
	}
}

func TestLinkRejectsBadInput(t *testing.T) {
	dbPath := testdb.Path(t)
	id := addJob(t, dbPath)
	if _, err := run(t, dbPath, true, "link", "add", id, "--kind", "bogus", "--ref", "x"); code(err) != 3 {
		t.Errorf("bad kind code = %d", code(err))
	}
	if _, err := run(t, dbPath, true, "link", "add", id, "--kind", "person", "--ref", strings.Repeat("x", 500)); code(err) != 3 {
		t.Errorf("long ref code = %d", code(err))
	}
	if _, err := run(t, dbPath, true, "link", "add", "nope", "--kind", "person", "--ref", "x"); code(err) != 2 {
		t.Errorf("unknown app code = %d", code(err))
	}
}

func TestFillRecordAndShow(t *testing.T) {
	dbPath := homeDB(t)
	id := addJob(t, dbPath)
	root := profiledir.Root(nil, "default")
	os.MkdirAll(root, 0o700)
	shot := filepath.Join(root, "shot.png")
	os.WriteFile(shot, []byte("png"), 0o600)
	fields := filepath.Join(t.TempDir(), "f.json")
	os.WriteFile(fields, []byte(`[{"label":"Email","value":"a@b.example","source":"profile"},{"label":"Password","value":"hunter2"}]`), 0o600)

	out, err := run(t, dbPath, true, "fill", "record", id, "--status", "needs_you", "--screenshot", shot, "--note", "captcha", "--fields-json", fields)
	if err != nil {
		t.Fatalf("fill record: %v (%s)", err, out)
	}
	show, err := run(t, dbPath, true, "fill", "show", id)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Status, Screenshot, Note string
		Fields                   []struct{ Label, Value string }
	}
	if err := json.Unmarshal([]byte(show), &f); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(shot)
	if f.Status != "needs_you" || f.Screenshot != real || f.Note != "captcha" || len(f.Fields) != 2 || f.Fields[1].Value != "[redacted]" {
		t.Fatalf("fill = %+v (%s)", f, show)
	}
	if strings.Contains(show, "hunter2") {
		t.Fatal("a secret-shaped value was stored")
	}
}

func TestFillRecordRefusesPathsOutsideProfileFolder(t *testing.T) {
	dbPath := homeDB(t)
	id := addJob(t, dbPath)
	root := profiledir.Root(nil, "default")
	os.MkdirAll(root, 0o700)
	outside := filepath.Join(t.TempDir(), "x.png")
	os.WriteFile(outside, []byte("png"), 0o600)
	link := filepath.Join(root, "link.png")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{
		"outside": outside, "symlink-out": link, "traversal": filepath.Join(root, "..", "x.png"),
		"dir": root, "missing": filepath.Join(root, "nope.png"),
	} {
		if _, err := run(t, dbPath, true, "fill", "record", id, "--status", "filled", "--screenshot", p); code(err) != 3 {
			t.Errorf("%s: code = %d, want 3", name, code(err))
		}
	}
	if out, _ := run(t, dbPath, true, "fill", "show", id); !strings.Contains(out, `"none"`) {
		t.Fatalf("a refused record was stored: %s", out)
	}
}

func TestFillRecordRefusesBadStatusAndBadFieldsFile(t *testing.T) {
	dbPath := homeDB(t)
	id := addJob(t, dbPath)
	if _, err := run(t, dbPath, true, "fill", "record", id, "--status", "bogus"); code(err) != 3 {
		t.Errorf("bad status code = %d", code(err))
	}
	bad := filepath.Join(t.TempDir(), "f.json")
	os.WriteFile(bad, []byte(`{"not":"a list"}`), 0o600)
	if _, err := run(t, dbPath, true, "fill", "record", id, "--status", "filled", "--fields-json", bad); code(err) != 3 {
		t.Errorf("bad fields code = %d", code(err))
	}
	big := filepath.Join(t.TempDir(), "big.json")
	os.WriteFile(big, []byte(`[{"label":"a","value":"`+strings.Repeat("v", 5000)+`"}]`), 0o600)
	if _, err := run(t, dbPath, true, "fill", "record", id, "--status", "filled", "--fields-json", big); code(err) != 3 {
		t.Errorf("long value code = %d", code(err))
	}
	if _, err := run(t, dbPath, true, "fill", "record", "nope", "--status", "filled"); code(err) != 2 {
		t.Errorf("unknown app code = %d", code(err))
	}
}

func TestStatusSetReachesNewStatuses(t *testing.T) {
	dbPath := testdb.Path(t)
	id := addJob(t, dbPath)
	for _, s := range []string{"needs_you", "awaiting_submit"} {
		if out, err := run(t, dbPath, true, "status", id, "set", s); err != nil {
			t.Fatalf("status set %s: %v (%s)", s, err, out)
		}
	}
	if out, _ := run(t, dbPath, true, "get", id); !strings.Contains(out, "awaiting_submit") {
		t.Fatalf("get: %s", out)
	}
}
