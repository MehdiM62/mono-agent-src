package browserjev

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/testdb"
	"github.com/monoes/mono-agent/internal/vault"
)

// fakeGuardPage records submit_guard calls and answers with state.
type fakeGuardPage struct {
	state   extension.SubmitGuardState
	actions []string
	evals   []string
	evalOK  bool
}

func (p *fakeGuardPage) SubmitGuard(action string) (*extension.SubmitGuardState, error) {
	p.actions = append(p.actions, action)
	if action == extension.SubmitGuardArm {
		p.state.Armed = true
	}
	st := p.state
	return &st, nil
}

func (p *fakeGuardPage) Screenshot(extension.ScreenshotOptions) (*extension.Screenshot, error) {
	return &extension.Screenshot{Data: base64.StdEncoding.EncodeToString([]byte("png")), MIME: "image/png"}, nil
}

func (p *fakeGuardPage) EvalCDP(js string) (interface{}, error) {
	p.evals = append(p.evals, js)
	return p.evalOK, nil
}

func (p *fakeGuardPage) TabID() int { return 41 }

func blockedAt(seq int64, url string) extension.BlockedSubmit {
	return extension.BlockedSubmit{Seq: seq, URL: url, Form: extension.SubmitForm{Action: url, Method: "post", Submitter: "#send",
		Fields: []extension.SubmitField{{Name: "msg", Type: "text", Value: "hi"}, {Name: "pw", Type: "password", Value: "•••"}}}}
}

func TestExtGuardHoldsSubmitsAsBrowserSubmitItems(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	db := testdb.Open(t).DB
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db), "p1")
	if _, err := armGuard(context.Background(), &fakeGuardPage{}); err == nil {
		t.Error("no database to hold approvals: arming must fail")
	}

	page := &fakeGuardPage{state: extension.SubmitGuardState{Blocked: []extension.BlockedSubmit{blockedAt(3, "https://old.example.com/")}}}
	g, err := armGuard(ctx, page)
	if err != nil || page.actions[0] != extension.SubmitGuardArm || g.seen != 3 {
		t.Fatalf("arm: %v %v seen=%d", err, page.actions, g.seen)
	}
	if got, _ := g.Blocked(ctx); len(got) != 0 {
		t.Errorf("a record from before the run is not the run's: %+v", got)
	}
	page.state.Blocked = append(page.state.Blocked, blockedAt(4, "https://shop.example.com/cart"))
	got, err := g.Blocked(ctx)
	if err != nil || len(got) != 1 || got[0].Seq != 4 || got[0].Form.Submitter != "#send" || got[0].Form.Fields[1].Value != "•••" {
		t.Fatalf("blocked = %+v, %v", got, err)
	}

	id, err := g.Ask(ctx, got[0], hilsend.PreparedBy{AgentID: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	it, err := browserctl.GetSubmitItem(ctx, db, "p1", id)
	if err != nil || it.Status != "pending" || it.TabID != 41 || it.Via != "guard" || it.Seq != 4 || it.Origin != "https://shop.example.com" {
		t.Fatalf("item = %+v, %v", it, err)
	}
	if _, err := os.Stat(it.Screenshot); err != nil || !strings.Contains(it.Screenshot, ".monoagent/browser/hil/") {
		t.Errorf("card screenshot %q: %v", it.Screenshot, err)
	}
	if st, _ := g.Decision(ctx, id); st != "pending" {
		t.Errorf("decision = %s", st)
	}
	if _, err := db.Exec(`UPDATE hil_pending SET status = 'approved' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if st, _ := g.Decision(ctx, id); st != "approved" {
		t.Errorf("decision = %s", st)
	}

	page.evalOK, page.actions = true, nil
	if err := g.Submit(ctx, got[0], id); err != nil {
		t.Fatal(err)
	}
	if strings.Join(page.actions, ",") != extension.SubmitGuardAllowOnce || len(page.evals) != 1 || !strings.Contains(page.evals[0], `"#send"`) {
		t.Errorf("submit: actions=%v evals=%v", page.actions, page.evals)
	}
	if it, _ := browserctl.GetSubmitItem(ctx, db, "p1", id); it.Status != "consumed" || it.SubmittedAt == "" {
		t.Errorf("an approval is used once: %+v", it)
	}
	page.actions = nil
	if err := g.Submit(ctx, got[0], id); err != nil || len(page.actions) != 0 {
		t.Errorf("an item already used is not sent again: %v %v", err, page.actions)
	}
}

func TestExtGuardRearmsAndRecoversRecords(t *testing.T) {
	db := testdb.Open(t).DB
	ctx := vault.ContextWithDB(context.Background(), db)
	page := &fakeGuardPage{}
	g, err := armGuard(ctx, page)
	if err != nil {
		t.Fatal(err)
	}
	page.state.Blocked = []extension.BlockedSubmit{blockedAt(9, "https://a.example.com/")}
	if got, _ := g.Blocked(ctx); len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	// The extension restarted: the guard is off and its records start over.
	page.state = extension.SubmitGuardState{Blocked: []extension.BlockedSubmit{blockedAt(1, "https://a.example.com/")}}
	page.actions = nil
	got, _ := g.Blocked(ctx)
	if len(got) != 1 || got[0].Seq != 1 || !strings.Contains(strings.Join(page.actions, ","), extension.SubmitGuardArm) {
		t.Errorf("after a restart: blocked=%+v actions=%v", got, page.actions)
	}

	// A submit that can no longer be repeated holds the next one again.
	page.actions, page.evalOK = nil, false
	if err := g.Submit(ctx, got[0], ""); err == nil || strings.Join(page.actions, ",") != "allow_once,arm" {
		t.Errorf("err=%v actions=%v", err, page.actions)
	}
}
