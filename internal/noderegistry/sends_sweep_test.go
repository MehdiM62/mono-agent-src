package noderegistry

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/apipkg"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/workflow"
)

// proseKeys mirror hilsend's: fields that say what a send says.
var proseKeys = map[string]bool{"subject": true, "body": true, "text": true, "html": true, "message": true, "content": true, "title": true, "description": true}

// strays are the shapes an address or a phone number takes in a field
// (security review of #871 round 3: the net read ASCII emails and +phones
// only).
var strays = []string{"evil@attacker.test", "\u00fc@ex\u00e4mple.de", "evil@intranet", "evil@[10.0.0.1]",
	"5550100199", "0044 20 7946 0000", "+1 (555) 010-0199", "evil\uff20attacker.test", "evil%40attacker.test"}

// sweepOp checks one send op: with an always-allow rule for its recipient,
// the plain send goes out, and the same send with an address (each of
// strays) in any other field of its schema (not its recipient, text,
// subject or sender) gets a card. It reports whether the op stands for a
// rule at all.
func sweepOp(t *testing.T, db *sql.DB, key, nodeType string, base map[string]interface{}, skip map[string]bool, fields []workflow.NodeSchemaField) (ruled bool, checked int) {
	t.Helper()
	st := hilsend.Store{DB: db, Profile: "default"}
	// Each op starts with no rule: another op's rule on the same platform
	// would let this one's plain send go before it is checked.
	rules, _ := st.Allows(context.Background())
	for _, r := range rules {
		if _, err := st.Disallow(context.Background(), r.ID); err != nil {
			t.Fatal(err)
		}
	}
	guard := func(cfg map[string]interface{}) (bool, hilsend.Payload) {
		out, handled, err := hilsend.Guard(sendsCtx(db), nodeType, workflow.NodeInput{}, cfg)
		if err != nil || !handled {
			return false, hilsend.Payload{}
		}
		p, _ := st.Get(context.Background(), out[0].Items[0].JSON["hil_id"].(string))
		return true, p
	}
	h, card := guard(base)
	if !h || card.Recipient.Label {
		return false, 0
	}
	if err := st.Allow(context.Background(), hilsend.Allow{Action: card.Action, Platform: card.Account.Platform, Account: card.Account.Handle, Recipient: "ada@example.test"}); err != nil {
		t.Fatal(err)
	}
	if h, _ := guard(base); h {
		return false, 0 // no rule stands for it (a reply, a draft, a call, no recipient param)
	}
	for _, f := range fields {
		k := f.Key
		collection := f.Type == "collection" || f.Type == "fixedCollection" || f.Type == "json" || f.Type == "code"
		// Only a string under a prose key is prose: an object there is
		// read like any other (Graph's message holds recipients).
		if k == "" || skip[k] || !collection && proseKeys[strings.ToLower(k)] || hilsend.SenderKey(nodeType, k) {
			continue
		}
		for _, stray := range strays {
			cfg := cloneDeep(base)
			if collection {
				// A collection holds options: add one beside the ones the
				// op's conditions set (options.sendUpdates), never in their
				// place.
				setParam(cfg, k+".email", stray)
			} else {
				setParam(cfg, k, stray)
			}
			if h, _ := guard(cfg); !h {
				t.Errorf("%s: %q in %q (%s) went out on the rule for the recipient", key, stray, k, f.Type)
			}
		}
		checked++
	}
	// A copy by id has no address for the net to read: its key alone gets
	// the card (ccs, bccs, watchers, at any depth).
	for _, k := range []string{"ccs", "bccs", "watchers", "options.watcherIds"} {
		cfg := cloneDeep(base)
		setParam(cfg, k, []interface{}{"5b10ac8d82e05b22cc7d4ef5"})
		if h, _ := guard(cfg); !h {
			t.Errorf("%s: a copy by id in %q went out on the rule for the recipient", key, k)
		}
	}
	return true, checked
}

// Every send op, an API package's annotated ones and the native nodes',
// with a rule for its one recipient: only that recipient goes without a
// card (third security review of #871: Mailgun's ccEmail, Mandrill's
// bcc_address and the like went out on a rule for the To).
func TestEverySendOpStandsForItsOneRecipientOnly(t *testing.T) {
	asOperator(t)
	t.Setenv(hilsend.EnvAgentContext, "coder")
	Build(nil)
	pkgs, err := apipkg.Load()
	if err != nil {
		t.Fatal(err)
	}
	db := sendsDB(t)
	ops, ruled, fields := 0, 0, 0
	for _, p := range pkgs {
		nodeType := p.NodeTypeOf()
		if p.Extends != "" {
			nodeType = p.Extends
		}
		for _, o := range p.Operations {
			s := o.Sends
			if s == nil || s.None || len(s.To) == 0 {
				continue
			}
			key := p.ID + " " + o.Resource + "/" + o.Operation
			base := map[string]interface{}{"resource": o.Resource, "operation": o.Operation}
			skip := map[string]bool{"resource": true, "operation": true, "credential_id": true, "auth": true}
			for _, cond := range s.If {
				name := cond
				for _, sep := range []string{" not in ", "!=", "="} {
					if n, v, ok := strings.Cut(cond, sep); ok {
						name = strings.TrimSuffix(n, "!")
						switch sep {
						case " not in ":
							setParam(base, name, "other")
						case "=":
							setParam(base, name, v)
						}
						break
					}
				}
				if !strings.ContainsAny(cond, "=") && !strings.Contains(cond, " not in ") {
					setParam(base, name, true)
				}
				skip[name] = true
			}
			setParam(base, s.To[0], "ada@example.test")
			for _, k := range append(append(append([]string{}, s.To...), s.Text...), s.Subject...) {
				skip[k] = true
			}
			if len(s.Text) > 0 {
				setParam(base, s.Text[0], "Hi Ada")
			}
			var visible []workflow.NodeSchemaField
			for _, f := range o.Fields {
				if len(f.VisibleFor) == 0 || visibleFor(f, o.Resource, o.Operation) {
					visible = append(visible, f)
				}
			}
			ops++
			if r, n := sweepOp(t, db, key, nodeType, base, skip, visible); r {
				ruled++
				fields += n
			}
		}
	}
	for _, n := range hilsend.NativeSends() {
		if len(n.To) == 0 {
			continue
		}
		schema, err := workflow.LoadDefaultSchema(n.NodeType)
		if err != nil {
			t.Fatalf("%s: %v", n.NodeType, err)
		}
		opsOf := n.Ops
		if opsOf == nil {
			opsOf = []string{""}
		}
		for _, op := range opsOf {
			base := map[string]interface{}{"to": nil}
			delete(base, "to")
			if op != "" {
				base["operation"] = op
			}
			setParam(base, n.To[0], "ada@example.test")
			skip := map[string]bool{"resource": true, "operation": true, "credential_id": true, "auth": true}
			for _, k := range append(append(append([]string{}, n.To...), n.Text...), n.Subject...) {
				skip[k] = true
			}
			if len(n.Text) > 0 {
				setParam(base, n.Text[0], "Hi Ada")
			}
			ops++
			if r, c := sweepOp(t, db, n.NodeType+" "+op, n.NodeType, base, skip, schema.Fields); r {
				ruled++
				fields += c
			}
		}
	}
	t.Logf("%d send ops, %d stand for a rule, %d fields checked", ops, ruled, fields)
	if ruled < 200 || fields < 900 {
		t.Errorf("only %d ops stood for a rule and %d fields were checked: the sweep lost its subjects?", ruled, fields)
	}
}

// cloneDeep copies a config, its collections included.
func cloneDeep(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		if sub, ok := v.(map[string]interface{}); ok {
			v = cloneDeep(sub)
		}
		out[k] = v
	}
	return out
}

// visibleFor reports whether f shows for this resource and op.
func visibleFor(f workflow.NodeSchemaField, resource, operation string) bool {
	for _, v := range f.VisibleFor {
		if (v["resource"] == "" || v["resource"] == resource) && (v["operation"] == "" || v["operation"] == operation) {
			return true
		}
	}
	return false
}
