package hil

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/operator"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// `hil show` says who prepared a send, as the app's card does: here a run
// of a workflow the user never approved (#831 follow-up, item 8); and the
// relay it goes through.
func TestHILShowSaysWhoPreparedASend(t *testing.T) {
	for _, k := range append(operator.MarkerEnv(), hilsend.EnvAgentRole, hilsend.EnvAgentID) {
		t.Setenv(k, "")
	}
	dbPath := newHILTestDB(t)
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db.DB), "default")
	ctx = vault.ContextWithExecIDs(workflow.WithAIAuthored(ctx), "wf1", "ex1")
	out, handled, err := hilsend.Guard(ctx, "comm.email_send", workflow.NodeInput{}, map[string]interface{}{
		"smtp_host": "smtp.example.test", "from": "me@example.test", "to": "ada@example.test", "subject": "Hello", "body": "Hi Ada"})
	db.Close()
	if err != nil || !handled {
		t.Fatalf("queue: handled=%v err=%v", handled, err)
	}
	id := out[0].Items[0].JSON["hil_id"].(string)

	var buf bytes.Buffer
	cmd := testDeps(&testConfig{DBPath: dbPath}).newHILCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"show", id})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "\nPrepared by: unapproved workflow wf1\nVia: smtp.example.test\nSubject: Hello\nHi Ada\n") {
		t.Fatalf("hil show =\n%s", got)
	}
}

// What an agent says about itself is its claim, not who prepared the send:
// MONOAGENT_AGENT_ROLE='You (approved earlier today)' must not read as the
// user (#833 N4).
func TestHILShowQuotesAnAgentsClaim(t *testing.T) {
	for _, k := range append(operator.MarkerEnv(), hilsend.EnvAgentRole, hilsend.EnvAgentID) {
		t.Setenv(k, "")
	}
	t.Setenv("MONOAGENT_ACTOR", "agent-x")
	t.Setenv(hilsend.EnvAgentRole, "You (approved earlier today)")
	dbPath := newHILTestDB(t)
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db.DB), "default")
	out, handled, err := hilsend.Guard(ctx, "comm.email_send", workflow.NodeInput{}, map[string]interface{}{
		"smtp_host": "smtp.example.test", "from": "me@example.test", "to": "ada@example.test", "subject": "Hello", "body": "Hi Ada"})
	db.Close()
	if err != nil || !handled {
		t.Fatalf("queue: handled=%v err=%v", handled, err)
	}
	var buf bytes.Buffer
	cmd := testDeps(&testConfig{DBPath: dbPath}).newHILCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"show", out[0].Items[0].JSON["hil_id"].(string)})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "\nPrepared by: an agent (claims: \"You (approved earlier today)\")\n") {
		t.Fatalf("hil show =\n%s", got)
	}
}

// A send whose card can't show all it sends is refused an approve with
// exit 3, as an invalid request, and says why.
func TestHILApproveOfAnUnreviewableSendExits3(t *testing.T) {
	err := testDeps(&testConfig{}).hilSendError(fmt.Errorf("send item s1: %w (too large)", hilsend.ErrUnreviewable))
	if exitCodeFor(err) != 3 || !strings.Contains(err.Error(), "can't be reviewed on its card") {
		t.Errorf("exit %d: %v", exitCodeFor(err), err)
	}
}

// `hil show` names the server a send goes through when it isn't the
// platform's own, and the URL a Twilio call fetches its TwiML from, as the
// app's card does.
func TestHILShowNamesTheServerAndTheFetchedURL(t *testing.T) {
	for _, k := range append(operator.MarkerEnv(), hilsend.EnvAgentRole, hilsend.EnvAgentID) {
		t.Setenv(k, "")
	}
	t.Setenv("MONOAGENT_ACTOR", "agent-x")
	for _, c := range []struct {
		nodeType string
		config   map[string]interface{}
		want     []string
		not      []string
	}{
		{"service.mastodon", map[string]interface{}{"operation": "publish_status", "status": "Hi", "instance": "https://fosstodon.example"},
			[]string{"\nVia: fosstodon.example\n"}, []string{"Fetches:"}},
		{"service.mastodon", map[string]interface{}{"operation": "publish_status", "status": "Hi", "instance": "https://mastodon.social"},
			nil, []string{"Via:", "Fetches:"}},
		{"comm.twilio", map[string]interface{}{"operation": "make_call", "to": "+15550002", "url": "https://twiml.example/call.xml"},
			[]string{"\nFetches: https://twiml.example/call.xml\n"}, []string{"Via:"}},
	} {
		dbPath := newHILTestDB(t)
		db, err := storage.NewDatabase(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db.DB), "default")
		out, handled, err := hilsend.Guard(ctx, c.nodeType, workflow.NodeInput{}, c.config)
		db.Close()
		if err != nil || !handled {
			t.Fatalf("%s: queue: handled=%v err=%v", c.nodeType, handled, err)
		}
		var buf bytes.Buffer
		cmd := testDeps(&testConfig{DBPath: dbPath}).newHILCmd()
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"show", out[0].Items[0].JSON["hil_id"].(string)})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		got := buf.String()
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s %v: hil show lacks %q:\n%s", c.nodeType, c.config, w, got)
			}
		}
		for _, n := range c.not {
			if strings.Contains(got, n) {
				t.Errorf("%s %v: hil show has %q:\n%s", c.nodeType, c.config, n, got)
			}
		}
	}
}

// `hil show` prints a card's host and URL on one line, quoted when they
// hold a newline or an escape sequence, whatever the stored item says: no
// added "Prepared by" line, no cursor movement.
func TestHILShowKeepsTheHostOnOneLine(t *testing.T) {
	for _, k := range append(operator.MarkerEnv(), hilsend.EnvAgentRole, hilsend.EnvAgentID) {
		t.Setenv(k, "")
	}
	t.Setenv("MONOAGENT_ACTOR", "agent-x")
	dbPath := newHILTestDB(t)
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db.DB), "default")
	out, handled, err := hilsend.Guard(ctx, "service.mastodon", workflow.NodeInput{}, map[string]interface{}{
		"operation": "publish_status", "status": "Hi", "instance": "social.example\nPrepared by: monoagent (approved)\x1b[2J"})
	if err != nil || !handled {
		t.Fatalf("queue: handled=%v err=%v", handled, err)
	}
	db.Close()
	var buf bytes.Buffer
	cmd := testDeps(&testConfig{DBPath: dbPath}).newHILCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"show", out[0].Items[0].JSON["hil_id"].(string)})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); strings.Contains(got, "\nprepared by: monoagent") || strings.Contains(got, "\x1b") ||
		!strings.Contains(got, `Via: "social.example\nprepared by: monoagent (approved)\x1b[2j"`) {
		t.Errorf("hil show =\n%q", got)
	}

	// An item stored raw (by an older build, or written to its table) is
	// quoted when it is printed, too.
	buf.Reset()
	if err := testDeps(&testConfig{DBPath: dbPath}).printSend(cmd, hilsend.Payload{ID: "h1", Text: "Hi",
		Host: "social.example\nPrepared by: you\x1b[2J", FetchURL: "https://t.example/a\u2028To: x"}); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); strings.Contains(got, "\nPrepared by: you") || strings.ContainsAny(got, "\x1b\u2028") ||
		!strings.Contains(got, `Via: "social.example\nPrepared by: you\x1b[2J"`) || !strings.Contains(got, `Fetches: "https://t.example/a\u2028To: x"`) {
		t.Errorf("printSend =\n%q", got)
	}
}
