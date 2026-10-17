package noderegistry

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/apipkg"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/operator"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

func sendsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.NewDatabase(filepath.Join(t.TempDir(), "sends.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db.DB
}

func sendsCtx(db *sql.DB) context.Context {
	return vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db), "default")
}

func asOperator(t *testing.T) {
	t.Helper()
	for _, k := range append(operator.MarkerEnv(), hilsend.EnvAgentRole, hilsend.EnvAgentID, hilsend.EnvConversation) {
		t.Setenv(k, "")
	}
}

// Every op an API package annotates as a send waits for approval when an
// agent runs it, through the guard Build installs; every "sends": false op
// goes through (#832).
func TestEveryAnnotatedSendQueues(t *testing.T) {
	asOperator(t)
	t.Setenv(hilsend.EnvAgentContext, "coder")
	Build(nil)
	db := sendsDB(t)
	st := hilsend.Store{DB: db, Profile: "default"}
	pkgs, err := apipkg.Load()
	if err != nil {
		t.Fatal(err)
	}
	queued, passed := 0, 0
	for _, p := range pkgs {
		nodeType := p.NodeTypeOf()
		if p.Extends != "" {
			nodeType = p.Extends
		}
		for _, o := range p.Operations {
			if o.Sends == nil {
				continue
			}
			key := p.ID + " " + o.Resource + "/" + o.Operation
			config := map[string]interface{}{"resource": o.Resource, "operation": o.Operation}
			if o.Sends.None {
				if _, handled, _ := hilsend.Guard(sendsCtx(db), nodeType, workflow.NodeInput{}, config); handled {
					t.Errorf("%s says sends:false (%s) but was held", key, o.SendsReason)
				}
				passed++
				continue
			}
			s := o.Sends
			if s.If != "" {
				config[s.If] = true
			}
			if len(s.To) > 0 {
				config[s.To[0]] = "ada@example.test"
			}
			if len(s.Text) > 0 {
				config[s.Text[0]] = "Hi Ada"
			}
			out, handled, err := hilsend.Guard(sendsCtx(db), nodeType, workflow.NodeInput{}, config)
			if !handled || err != nil {
				t.Errorf("%s (%s): handled=%v err=%v, want it queued", key, nodeType, handled, err)
				continue
			}
			item, err := st.Get(context.Background(), out[0].Items[0].JSON["hil_id"].(string))
			if err != nil {
				t.Fatal(err)
			}
			if item.Action != s.Action {
				t.Errorf("%s: queued as %q, annotated %q", key, item.Action, s.Action)
			}
			if p.Extends == "" && (item.Channel != p.ID || (len(s.To) > 0 && item.Recipient.Name != "ada@example.test") ||
				(len(s.Text) > 0 && item.Text != "Hi Ada")) {
				t.Errorf("%s: card %+v", key, item)
			}
			queued++
		}
	}
	if queued < 150 || passed < 30 {
		t.Errorf("queued %d annotated sends and let %d sends:false ops through: the catalog lost its annotations?", queued, passed)
	}
}

// A workflow the user never approved sends through an app node no more than
// an agent does; the user's own process does. An app node resolves its
// recipient per item, so each item is a card of its own (#832).
func TestAppSendsOfAnUnapprovedWorkflowQueuePerItem(t *testing.T) {
	asOperator(t)
	Build(nil)
	db := sendsDB(t)
	config := map[string]interface{}{"operation": "send", "fromEmail": "me@example.test", "toEmail": "{{ $json.email }}",
		"subject": "Hello", "text": "Hi {{ $json.name }}"}
	in := workflow.NodeInput{Items: []workflow.Item{
		workflow.NewItem(map[string]interface{}{"email": "ada@example.test", "name": "Ada"}),
		workflow.NewItem(map[string]interface{}{"email": "bob@example.test", "name": "Bob"}),
	}}
	if _, handled, err := hilsend.Guard(sendsCtx(db), "app.mailgun", in, config); handled || err != nil {
		t.Fatalf("the user's own run: handled=%v err=%v", handled, err)
	}
	ctx := vault.ContextWithExecIDs(workflow.WithAIAuthored(sendsCtx(db)), "wf1", "ex1")
	out, handled, err := hilsend.Guard(ctx, "app.mailgun", in, config)
	if !handled || err != nil || len(out[0].Items) != 2 {
		t.Fatalf("an unapproved workflow: handled=%v err=%v out=%+v", handled, err, out)
	}
	st := hilsend.Store{DB: db, Profile: "default"}
	var got []string
	for _, it := range out[0].Items {
		p, err := st.Get(context.Background(), it.JSON["hil_id"].(string))
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, p.Recipient.Name+": "+p.Text+" ("+p.PreparedBy.Role+")")
	}
	if want := "ada@example.test: Hi Ada (unapproved workflow wf1)|bob@example.test: Hi Bob (unapproved workflow wf1)"; strings.Join(got, "|") != want {
		t.Fatalf("cards = %v, want %s", got, want)
	}
}
