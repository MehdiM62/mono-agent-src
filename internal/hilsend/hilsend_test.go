package hilsend

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/data"
	"github.com/monoes/mono-agent/internal/orgsign"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	for _, name := range []string{"001_initial.sql", "010_hil_pending.sql", "011_profiles.sql", "065_send_allow.sql"} {
		raw, err := data.MigrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if name == "011_profiles.sql" {
			// Only the hil_pending part of the profile migration matters here.
			raw = []byte("ALTER TABLE hil_pending ADD COLUMN profile_id TEXT NOT NULL DEFAULT 'default';")
		}
		if _, err := db.Exec(string(raw)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	return db
}

// asOperator clears every marker that makes a process an agent's.
func asOperator(t *testing.T) {
	t.Helper()
	for _, k := range append(orgsign.AgentContextMarkers(), EnvAgentContext, "MONOAGENT_ACTOR", EnvApprovalID, EnvApprovalProof,
		EnvConversation, EnvAgentRole, EnvAgentID, "MONOMIND_ORG_NAME", "MONOMIND_ORG_ROLE") {
		t.Setenv(k, "")
	}
}

func asAgent(t *testing.T) {
	t.Helper()
	asOperator(t)
	t.Setenv(EnvAgentContext, "coder")
	t.Setenv(EnvConversation, "conv-42")
	t.Setenv(EnvAgentRole, "outreach")
	t.Setenv(EnvAgentID, "lead")
}

func ctxFor(db *sql.DB) context.Context {
	return vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db), "p1")
}

var emailConfig = func() map[string]interface{} {
	return map[string]interface{}{
		"from": "me@example.test", "to": "ada@example.test",
		"subject": "Hello", "body": "Original body", "password": "hunter2",
		// The connection data a run merges in. It names no server: one that
		// a config names is its own only when it is its saved connection's
		// (ownServer, TestAServerOtherThanTheConnectionsGetsACard).
		"credential": map[string]interface{}{"password": "hunter2", "extra": "x"}, "extra": "x",
	}
}

type fakeRunner struct {
	calls []Run
	err   error
}

func (f *fakeRunner) run(_ context.Context, r Run) (json.RawMessage, error) {
	f.calls = append(f.calls, r)
	if f.err != nil {
		return nil, f.err
	}
	return json.RawMessage(`{"ok":true}`), nil
}

func queueEmail(t *testing.T, db *sql.DB) string {
	t.Helper()
	out, handled, err := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, emailConfig())
	if err != nil || !handled {
		t.Fatalf("Guard handled=%v err=%v", handled, err)
	}
	return out[0].Items[0].JSON["hil_id"].(string)
}

func TestOperatorSendsAreNotIntercepted(t *testing.T) {
	asOperator(t)
	db := testDB(t)
	_, handled, err := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, emailConfig())
	if handled || err != nil {
		t.Fatalf("operator context: handled=%v err=%v", handled, err)
	}
	if got, _ := (Store{db, "p1"}).List(context.Background()); len(got) != 0 {
		t.Fatalf("operator send queued an item: %+v", got)
	}
}

func TestAgentSendIsQueuedNotPerformed(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	out, handled, err := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, emailConfig())
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	j := out[0].Items[0].JSON
	if j["pending"] != true || j["status"] != "queued for approval" || j["success"] != false || j["hil_id"] == "" {
		t.Fatalf("output = %+v", j)
	}
	st := Store{db, "p1"}
	p, err := st.Get(context.Background(), j["hil_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != "send" || p.State != StatePending || p.Action != ActionEmail || p.Channel != "email" ||
		p.Text != "Original body" || p.Subject != "Hello" || p.Recipient.Name != "ada@example.test" ||
		p.Account.Handle != "me@example.test" || p.NodeType != "comm.email_send" || p.CreatedAt == "" {
		t.Fatalf("payload = %+v", p)
	}
	if p.PreparedBy.Role != "outreach" || p.PreparedBy.AgentID != "lead" || p.PreparedBy.ConversationID != "conv-42" {
		t.Fatalf("preparedBy = %+v", p.PreparedBy)
	}
	// It shows in the HIL queue as a pending row, with no secrets stored.
	var status, nc string
	if err := db.QueryRow(`SELECT status, node_config FROM hil_pending WHERE id = ?`, p.ID).Scan(&status, &nc); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || !IsSend(nc) {
		t.Fatalf("row = %s %s", status, nc)
	}
	if strings.Contains(nc, "hunter2") || strings.Contains(nc, `"extra":"x"`) && strings.Contains(nc, `"credential"`) {
		t.Fatalf("secrets stored in the replay: %s", nc)
	}
}

func TestApproveRunsOnceWithEditedText(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	id := queueEmail(t, db)
	st := Store{db, "p1"}
	f := &fakeRunner{}
	edited := "Edited body"
	p, err := Resolve(context.Background(), st, id, true, Edit{Text: &edited}, false, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if p.State != StateSent || p.Text != edited || len(f.calls) != 1 {
		t.Fatalf("state=%s text=%q calls=%d", p.State, p.Text, len(f.calls))
	}
	c := f.calls[0]
	if c.NodeType != "comm.email_send" || c.Config["body"] != edited || c.Config["subject"] != "Hello" || c.Config["to"] != "ada@example.test" {
		t.Fatalf("run = %+v", c)
	}
	if _, leaked := c.Config["password"]; leaked {
		t.Fatalf("secret reached the replay: %+v", c.Config)
	}
	// Approving again (double click, retry) is idempotent: nothing runs.
	again, err := Resolve(context.Background(), st, id, true, Edit{}, false, f.run)
	if err != nil || again.State != StateSent || len(f.calls) != 1 {
		t.Fatalf("second approve: state=%s calls=%d err=%v", again.State, len(f.calls), err)
	}
	// ...and a late reject cannot undo it.
	rej, err := Resolve(context.Background(), st, id, false, Edit{}, false, f.run)
	if err != nil || rej.State != StateSent {
		t.Fatalf("late reject: state=%s err=%v", rej.State, err)
	}
}

func TestRejectNeverRuns(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	id := queueEmail(t, db)
	st := Store{db, "p1"}
	f := &fakeRunner{}
	p, err := Resolve(context.Background(), st, id, false, Edit{}, false, f.run)
	if err != nil || p.State != StateRejected {
		t.Fatalf("reject: %+v %v", p, err)
	}
	p, err = Resolve(context.Background(), st, id, true, Edit{}, false, f.run)
	if err != nil || p.State != StateRejected || len(f.calls) != 0 {
		t.Fatalf("approve after reject: state=%s calls=%d err=%v", p.State, len(f.calls), err)
	}
}

func TestFailedSendIsFinal(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	id := queueEmail(t, db)
	st := Store{db, "p1"}
	f := &fakeRunner{err: errors.New("smtp refused")}
	p, err := Resolve(context.Background(), st, id, true, Edit{}, false, f.run)
	if err != nil || p.State != StateFailed || !strings.Contains(p.Error, "smtp refused") {
		t.Fatalf("failed send: %+v %v", p, err)
	}
	f.err = nil
	if p, _ = Resolve(context.Background(), st, id, true, Edit{}, false, f.run); p.State != StateFailed || len(f.calls) != 1 {
		t.Fatalf("a failed send ran again: state=%s calls=%d", p.State, len(f.calls))
	}
}

func TestEditKeepsPendingAndRePreviews(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	id := queueEmail(t, db)
	st := Store{db, "p1"}
	text, subj := "New text", "New subject"
	p, err := st.Edit(context.Background(), id, &text, &subj)
	if err != nil || p.State != StatePending || p.Text != text || p.Subject != subj {
		t.Fatalf("edit: %+v %v", p, err)
	}
	got, _ := st.Get(context.Background(), id)
	if got.Text != text || got.Subject != subj {
		t.Fatalf("stored = %+v", got)
	}
	f := &fakeRunner{}
	if _, err := Resolve(context.Background(), st, id, true, Edit{}, false, f.run); err != nil {
		t.Fatal(err)
	}
	if f.calls[0].Config["body"] != text || f.calls[0].Config["subject"] != subj {
		t.Fatalf("approved payload is not the previewed one: %+v", f.calls[0].Config)
	}
}

func TestApprovedProcessPassesTheGuardOnce(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	id := queueEmail(t, db)
	st := Store{db, "p1"}
	var proof string
	run := func(ctx context.Context, r Run) (json.RawMessage, error) {
		proof = r.Proof
		// While the claimed send runs, the process holding the proof passes...
		t.Setenv(EnvApprovalID, r.ApprovalID)
		t.Setenv(EnvApprovalProof, r.Proof)
		_, handled, err := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, r.Config)
		if handled || err != nil {
			t.Errorf("approved process was intercepted: handled=%v err=%v", handled, err)
		}
		// ...but not for another node type, nor with a wrong proof.
		if _, handled, _ := Guard(ctxFor(db), "comm.outlook_send", workflow.NodeInput{}, r.Config); !handled {
			t.Error("the approval carried over to a different node type")
		}
		t.Setenv(EnvApprovalProof, "wrong")
		if _, handled, _ := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, r.Config); !handled {
			t.Error("a wrong proof passed the guard")
		}
		return nil, nil
	}
	if _, err := Resolve(context.Background(), st, id, true, Edit{}, false, run); err != nil {
		t.Fatal(err)
	}
	// Once sent, the same id and proof no longer pass.
	t.Setenv(EnvApprovalID, id)
	t.Setenv(EnvApprovalProof, proof)
	if _, handled, _ := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, emailConfig()); !handled {
		t.Error("a finished approval still passes the guard")
	}
}

func TestSettingOffAndFailClosed(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	if _, err := db.Exec(`INSERT INTO configs (name, config_data) VALUES ('coder_mode', '{"enabled":true,"sendApproval":false}')`); err != nil {
		t.Fatal(err)
	}
	if _, handled, err := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, emailConfig()); handled || err != nil {
		t.Fatalf("setting off: handled=%v err=%v", handled, err)
	}
	if _, err := db.Exec(`UPDATE configs SET config_data = '{"enabled":true}' WHERE name = 'coder_mode'`); err != nil {
		t.Fatal(err)
	}
	if _, handled, _ := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, emailConfig()); !handled {
		t.Fatal("an unset setting must mean on")
	}
	// No database: the send is refused, never run.
	if _, handled, err := Guard(context.Background(), "comm.email_send", workflow.NodeInput{}, emailConfig()); !handled || err == nil {
		t.Fatalf("no db: handled=%v err=%v", handled, err)
	}
}

func TestOnlySendingOperationsAreGated(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	for _, c := range []struct {
		node string
		cfg  map[string]interface{}
		want bool
	}{
		{"core.set", nil, false},
		{"service.outlook_mail", map[string]interface{}{"operation": "list_messages"}, false},
		{"service.outlook_mail", map[string]interface{}{"operation": "create_draft"}, false},
		{"service.outlook_mail", map[string]interface{}{"operation": "send_message", "to": "a@b.test"}, true},
		{"comm.slack", map[string]interface{}{"operation": "list_channels"}, false},
		{"comm.slack", map[string]interface{}{"operation": "post_message", "channel": "#g", "text": "hi"}, true},
		{"instagram.scrape_profile_info", nil, false},
		{"instagram.send_dms", map[string]interface{}{"messageText": "hi", "selectedListItems": []interface{}{"https://www.instagram.com/ada/"}}, true},
		{"comm.telegram", map[string]interface{}{"message": "hi", "chat_id": "1"}, true},
	} {
		_, handled, err := Guard(ctxFor(db), c.node, workflow.NodeInput{}, c.cfg)
		if handled != c.want || (c.want && err != nil) {
			t.Errorf("%s %v: handled=%v err=%v, want handled=%v", c.node, c.cfg["operation"], handled, err, c.want)
		}
	}
}

func TestSocialBatchBecomesOneItemPerRecipient(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	if _, err := db.Exec(`INSERT INTO people (id, platform_username, platform, full_name) VALUES ('p9', 'ada', 'INSTAGRAM', 'Ada Lovelace')`); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]interface{}{
		"messageText": "Hi there", "account": "acme",
		"selectedListItems": []interface{}{"https://www.instagram.com/ada/", "https://www.instagram.com/bob/"},
	}
	out, handled, err := Guard(ctxFor(db), "instagram.send_dms", workflow.NodeInput{}, cfg)
	if err != nil || !handled || len(out[0].Items) != 2 {
		t.Fatalf("handled=%v err=%v out=%+v", handled, err, out)
	}
	st := Store{db, "p1"}
	list, _ := st.List(context.Background(), StatePending)
	if len(list) != 2 {
		t.Fatalf("items = %+v", list)
	}
	ada := list[0]
	if ada.Action != ActionDM || ada.Channel != "instagram" || ada.Account.Handle != "acme" || ada.Recipient.PersonID != "p9" ||
		ada.Recipient.Name != "ada" || ada.Recipient.PersonName != "Ada Lovelace" || ada.Recipient.ProfileURL != "https://www.instagram.com/ada/" || ada.Text != "Hi there" {
		t.Fatalf("ada = %+v", ada)
	}
	if list[1].Recipient.Name != "bob" || list[1].Recipient.PersonID != "" {
		t.Fatalf("bob = %+v", list[1].Recipient)
	}
	// Approving one performs only that recipient.
	f := &fakeRunner{}
	if _, err := Resolve(context.Background(), st, list[1].ID, true, Edit{}, false, f.run); err != nil {
		t.Fatal(err)
	}
	got := f.calls[0].Config["selectedListItems"].([]interface{})
	if len(f.calls) != 1 || len(got) != 1 || got[0] != "https://www.instagram.com/bob/" {
		t.Fatalf("calls = %+v", f.calls)
	}
}

func TestAlwaysAllowIsPerDestinationAndOperatorMade(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	st := Store{db, "p1"}
	id := queueEmail(t, db)
	f := &fakeRunner{}
	p, err := Resolve(context.Background(), st, id, true, Edit{}, true, f.run)
	if err != nil || p.State != StateSent || !p.AlwaysAllowed {
		t.Fatalf("approve+always: %+v %v", p, err)
	}
	// The same destination now goes straight through...
	if _, handled, err := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, emailConfig()); handled || err != nil {
		t.Fatalf("allowed destination queued: handled=%v err=%v", handled, err)
	}
	// ...another recipient, another action or another account still waits.
	other := emailConfig()
	other["to"] = "someone.else@example.test"
	if _, handled, _ := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, other); !handled {
		t.Fatal("always-allow leaked to another recipient")
	}
	acct := emailConfig()
	acct["from"] = "other@example.test"
	if _, handled, _ := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, acct); !handled {
		t.Fatal("always-allow leaked to another account")
	}
	rules, _ := st.Allows(context.Background())
	if len(rules) != 1 || rules[0].Recipient != "ada@example.test" {
		t.Fatalf("rules = %+v", rules)
	}
	if ok, _ := st.Disallow(context.Background(), rules[0].ID); !ok {
		t.Fatal("disallow")
	}
	if _, handled, _ := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, emailConfig()); !handled {
		t.Fatal("a removed rule still allows")
	}
}

func TestProfilesDoNotSeeEachOthersItems(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	id := queueEmail(t, db)
	if _, err := (Store{db, "other"}).Get(context.Background(), id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other profile: %v", err)
	}
	if _, err := Resolve(context.Background(), Store{db, "other"}, id, true, Edit{}, false, (&fakeRunner{}).run); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other profile approve: %v", err)
	}
}

func TestAttachmentsDescribedFromDiskAndPathConfined(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	root := t.TempDir()
	outside := t.TempDir()
	img := filepath.Join(root, "cover.png")
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(img, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	cfg := emailConfig()
	cfg["attachments"] = []interface{}{img, secret, "https://example.test/a/brochure.pdf", filepath.Join(root, "link.txt"), "../../etc/passwd", "sub/../cover.png"}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	out, _, err := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := (Store{db, "p1"}).Get(context.Background(), out[0].Items[0].JSON["hil_id"].(string))
	if len(p.Attachments) != 6 {
		t.Fatalf("attachments = %+v", p.Attachments)
	}
	if a := p.Attachments[0]; a.Kind != "image" || a.Mime != "image/png" || a.Size != 5 || a.Name != "cover.png" {
		t.Fatalf("image = %+v", a)
	}
	if a := p.Attachments[2]; a.Kind != "link" || a.URL == "" || a.Path != "" {
		t.Fatalf("link = %+v", a)
	}
	roots := []string{root}
	if f, err := AttachmentFile(p, 0, roots); err != nil || f.Size != 5 {
		t.Fatalf("inside root: %+v %v", f, err)
	}
	for _, n := range []int{1, 3, 4} {
		if _, err := AttachmentFile(p, n, roots); err == nil {
			t.Errorf("attachment %d (%s) outside the roots was served", n, p.Attachments[n].Path)
		}
	}
	if _, err := AttachmentFile(p, 2, roots); err == nil {
		t.Error("a link has no file")
	}
	if _, err := AttachmentFile(p, 9, roots); err == nil {
		t.Error("out of range")
	}
	// A relative path is taken from the first root; ".." that stays inside is fine.
	if f, err := AttachmentFile(p, 5, roots); err != nil || f.Size != 5 {
		t.Errorf("relative inside root: %+v %v", f, err)
	}
}

// A run with no profile has no queue to wait in: an agent send fails closed
// rather than being filed under a default profile.
func TestAgentSendWithoutAProfileFailsClosed(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	ctx := vault.ContextWithDB(context.Background(), db)
	_, handled, err := Guard(ctx, "comm.email_send", workflow.NodeInput{}, emailConfig())
	if err == nil || !handled {
		t.Fatalf("handled=%v err=%v, want a handled error", handled, err)
	}
}
