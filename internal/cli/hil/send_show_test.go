package hil

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/operator"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// `hil show` says who prepared a send, as the app's card does: here a run
// of a workflow the user never approved (#831 follow-up, item 8).
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
	if got := buf.String(); !strings.Contains(got, "\nPrepared by: unapproved workflow wf1\nSubject: Hello\nHi Ada\n") {
		t.Fatalf("hil show =\n%s", got)
	}
}
