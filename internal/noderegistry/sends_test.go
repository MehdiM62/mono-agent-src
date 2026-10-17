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
			if len(s.If) > 0 {
				// "param=value" is set to its value, a bare param to true.
				if name, value, eq := strings.Cut(s.If[0], "="); eq {
					setParam(config, name, value)
				} else {
					setParam(config, name, true)
				}
			}
			to := s.ToUnset // the card's recipient
			if len(s.To) > 0 {
				setParam(config, s.To[0], "ada@example.test")
				to = strings.TrimSpace(s.ToLabel[s.To[0]] + " ada@example.test")
			}
			if len(s.Text) > 0 {
				setParam(config, s.Text[0], "Hi Ada")
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
			if p.Extends == "" && (item.Channel != p.ID || item.Recipient.Name != to ||
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

// setParam sets a sends param: for "a.b", option b of collection a.
func setParam(config map[string]interface{}, name string, v interface{}) {
	head, rest, dotted := strings.Cut(name, ".")
	if !dotted {
		config[name] = v
		return
	}
	sub, _ := config[head].(map[string]interface{})
	if sub == nil {
		sub = map[string]interface{}{}
		config[head] = sub
	}
	setParam(sub, rest, v)
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

// An op that sends only with some param waits only when one is set: a
// calendar event that invites no one goes through, one inviting Ada waits
// with her on the card, and so does an invoice that is emailed as it is
// created.
func TestAConditionalSendWaitsOnlyWhenItSends(t *testing.T) {
	asOperator(t)
	t.Setenv(hilsend.EnvAgentContext, "coder")
	Build(nil)
	db := sendsDB(t)
	held := func(nodeType string, config map[string]interface{}) (string, bool) {
		t.Helper()
		out, handled, err := hilsend.Guard(sendsCtx(db), nodeType, workflow.NodeInput{}, config)
		if err != nil {
			t.Fatal(err)
		}
		if !handled {
			return "", false
		}
		p, err := (hilsend.Store{DB: db, Profile: "default"}).Get(context.Background(), out[0].Items[0].JSON["hil_id"].(string))
		if err != nil {
			t.Fatal(err)
		}
		return p.Recipient.Name, true
	}
	event := map[string]interface{}{"resource": "event", "operation": "create", "calendar": "primary",
		"additionalFields": map[string]interface{}{"summary": "Focus time"}}
	if _, ok := held("app.google_calendar", event); ok {
		t.Error("an event that invites no one was held")
	}
	event["additionalFields"].(map[string]interface{})["attendees"] = "ada@example.test"
	if to, ok := held("app.google_calendar", event); !ok || to != "ada@example.test" {
		t.Errorf("an event inviting Ada: held=%v to=%q", ok, to)
	}
	invoice := map[string]interface{}{"resource": "invoice", "operation": "create", "apiVersion": "v5",
		"additionalFields": map[string]interface{}{"client": "c1", "emailInvoice": false}}
	if _, ok := held("app.invoice_ninja", invoice); ok {
		t.Error("an invoice created without emailing it was held")
	}
	invoice["additionalFields"].(map[string]interface{})["emailInvoice"] = true
	if _, ok := held("app.invoice_ninja", invoice); !ok {
		t.Error("an invoice emailed as it is created went out")
	}
}

