package hilsend

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// A node is classified under the type that runs it, by the static legacy
// table, so a caller with no registry (orgdecide's MaySend) knows a legacy
// name's sends too.
func TestALegacyNameIsClassifiedAsItsCanonicalType(t *testing.T) {
	for _, c := range []struct {
		name   string
		config map[string]interface{}
		sends  bool
	}{
		{"telegram", map[string]interface{}{"chat_id": "42", "text": "Hi"}, true},
		{"email_send", map[string]interface{}{"to": "ada@example.test", "body": "Hi"}, true},
		{"gmail", map[string]interface{}{"operation": "send_message"}, true},
		{"gmail", map[string]interface{}{"operation": "list_messages"}, false},
		{"slack", map[string]interface{}{"operation": "post_message"}, true},
		{"discord", map[string]interface{}{"operation": "send_embed"}, true},
		{"comm.mastodon", map[string]interface{}{"operation": "create_status"}, true},
		{"service.mastodon", map[string]interface{}{"operation": "create_status"}, true},
		{"comm.reddit", map[string]interface{}{"operation": "reply_to_comment"}, true},
		{"service.reddit", map[string]interface{}{"operation": "reply_to_comment"}, true},
		{"comm.reddit", map[string]interface{}{"operation": "list_comments"}, false},
		{"comm.discord", map[string]interface{}{"channel_id": "C1", "content": "Hi"}, true},
		{"service.discord", map[string]interface{}{"channel_id": "C1", "content": "Hi"}, true},
		{"comm.discord", map[string]interface{}{"operation": "get_channels"}, false},
		{"comm.outlook_send", map[string]interface{}{"to": "ada@example.test", "subject": "Hello", "body": "Hi"}, true},
		{"service.outlook_mail", map[string]interface{}{"to": "ada@example.test", "subject": "Hello", "body": "Hi"}, true},
		{"comm.outlook_read", map[string]interface{}{"mailbox": "INBOX"}, false},
		{"comm.bluesky", map[string]interface{}{"operation": "create_post"}, true},
	} {
		if got := IsOutbound(c.name, c.config); got != c.sends {
			t.Errorf("IsOutbound(%s, %v) = %v, want %v", c.name, c.config, got, c.sends)
		}
		if got := MaySend(c.name, c.config); got != c.sends {
			t.Errorf("MaySend(%s, %v) = %v, want %v", c.name, c.config, got, c.sends)
		}
	}
	if !MaySend("discord", map[string]interface{}{"operation": "{{ $json.op }}"}) {
		t.Error("MaySend: a legacy name's operation only the run resolves")
	}
}

// The card of a send asked about by a legacy name names the canonical type,
// as the engine's do. An approval queued under the legacy name before (its
// replay names comm.mastodon) passes its approved run, which the guard now
// sees as service.mastodon; it still passes no other type.
func TestAnApprovalUnderALegacyNamePassesItsCanonicalRun(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	config := map[string]interface{}{"operation": "create_status", "status": "Hi all"}
	out, handled, err := Guard(ctxFor(db), "comm.mastodon", workflow.NodeInput{}, config)
	if err != nil || !handled {
		t.Fatalf("Guard handled=%v err=%v", handled, err)
	}
	id := out[0].Items[0].JSON["hil_id"].(string)
	st := Store{db, "p1"}
	if p, err := st.Get(context.Background(), id); err != nil || p.NodeType != "service.mastodon" {
		t.Fatalf("card: %v %+v, want node type service.mastodon", err, p)
	}
	if _, err := db.Exec(`UPDATE hil_pending SET node_config = replace(node_config, '"nodeType":"service.mastodon"', '"nodeType":"comm.mastodon"') WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	ran := false
	run := func(ctx context.Context, r Run) (json.RawMessage, error) {
		ran = true
		t.Setenv(EnvApprovalID, r.ApprovalID)
		t.Setenv(EnvApprovalProof, r.Proof)
		for _, name := range []string{r.NodeType, "service.mastodon"} {
			if _, handled, err := Guard(ctxFor(db), name, workflow.NodeInput{}, r.Config); handled || err != nil {
				t.Errorf("the approved run as %s was intercepted: handled=%v err=%v", name, handled, err)
			}
		}
		if _, handled, _ := Guard(ctxFor(db), "service.reddit", workflow.NodeInput{}, map[string]interface{}{"operation": "submit_post", "text": "Hi"}); !handled {
			t.Error("the approval carried over to a different node type")
		}
		return nil, nil
	}
	if _, err := Resolve(context.Background(), st, id, true, Edit{}, false, run); err != nil || !ran {
		t.Fatalf("approve: %v (ran %v)", err, ran)
	}
}
