package noderegistry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/workflow"
)

// nativeSends are the operations of the built-in comm.* and service.* node
// types that send a message or post, read from each node's code; "" is a
// config that names no operation (a legacy one), where the node's default
// sends. nativeReads are their other operations, reviewed as sending
// nothing to a person: reads, drafts, and engagements without the user's
// text (a like, a repost, a boost), as socialActions leaves likes out.
var (
	nativeSends = map[string][]string{
		"comm.email_send":      {""},
		"comm.slack":           {"post_message", "upload_file"},
		"comm.telegram":        {"", "send_message", "send_photo"},
		"comm.twilio":          {"send_sms", "send_whatsapp", "make_call"},
		"comm.whatsapp":        {"send_message", "send_template", "send_media"},
		"service.bluesky":      {"create_post"},
		"service.devto":        {"publish_article", "create_comment"},
		"service.discord":      {"", "send_message", "send_embed"},
		"service.gmail":        {"send_message", "send_draft", "reply"},
		"service.hashnode":     {"publish_post"},
		"service.mastodon":     {"publish_status", "create_status"},
		"service.outlook_mail": {"", "send_message", "send_draft", "reply"},
		"service.producthunt":  {"create_comment"},
		"service.reddit":       {"submit_post", "comment", "reply_to_comment"},
		"service.youtube":      {"upload_video", "reply_to_comment"},
	}
	nativeReads = map[string][]string{
		"comm.slack":           {"list_channels", "get_user"},
		"comm.telegram":        {"get_updates"},
		"service.bluesky":      {"get_timeline", "get_profile", "like_post", "repost", "get_post_metrics"},
		"service.devto":        {"list_articles", "get_article", "list_comments"},
		"service.discord":      {"list_messages", "add_reaction", "list_channels", "get_channels"},
		"service.gmail":        {"create_draft", "create_reply", "list_messages", "get_message", "list_labels", "trash_message"},
		"service.hashnode":     {"list_posts", "get_post"},
		"service.mastodon":     {"get_timeline", "get_account", "favourite", "boost", "get_status_metrics"},
		"service.outlook_mail": {"list_messages", "get_message", "create_draft", "create_reply", "delete_message", "whoami"},
		"service.producthunt":  {"get_post", "list_posts", "get_post_metrics"},
		"service.reddit":       {"get_hot", "upvote", "list_comments", "get_post_metrics"},
		"service.youtube":      {"get_video_stats", "list_comments", "search_videos"},
	}
	// sendsNothing are the comm.* and service.* types with no operation
	// that sends a message to a person. Their writes (an issue, a record, a
	// charge, a shared file) are acts on the outside world, which an org
	// grant's outbound check covers (orggrant.OutboundNodes), not sends.
	sendsNothing = []string{
		"comm.email_read", "service.airtable", "service.asana", "service.github", "service.google_drive",
		"service.google_sheets", "service.hubspot", "service.huggingface", "service.jira", "service.linear",
		"service.notion", "service.openrouter", "service.salesforce", "service.shopify", "service.stripe",
	}
)

// sendConfig is a config of op with a message's usual keys set.
func sendConfig(op string) map[string]interface{} {
	c := map[string]interface{}{"text": "Hi Ada", "body": "Hi Ada", "content": "Hi Ada", "message": "Hi Ada",
		"status": "Hi Ada", "subject": "Hello", "title": "Hello", "to": "ada@example.test", "chat_id": "42",
		"channel": "#general", "channel_id": "C1", "subreddit": "golang", "thing_id": "t1_abc", "video_path": "v.mp4"}
	if op != "" {
		c["operation"] = op
	}
	return c
}

// schemaOps are the operations a node type's own schema file offers (the
// built-in one, before any API package adds to it), nil without one.
func schemaOps(t *testing.T, nodeType string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "workflow", "schemas", nodeType+".json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var s workflow.NodeSchema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("%s: %v", nodeType, err)
	}
	var ops []string
	for _, f := range s.Fields {
		if f.Key == "operation" {
			ops = append(ops, f.Options...)
		}
	}
	return ops
}

// Every comm.* and service.* type Build registers, and every alias, is
// reviewed here, so a new alias, type or operation can't send unchecked:
//   - an alias resolves for the send gate (workflow.CanonicalNodeType) to the
//     type the registry runs it as;
//   - each type is in nativeSends or sendsNothing, and each operation its
//     schema files offer (its own and its legacy names') is in nativeSends or
//     nativeReads;
//   - each send waits for approval when an agent runs it, under the type's
//     name and every alias of it, through both callers of the guard: the
//     name as given (`node run`, MCP node_run) and the type of the executor
//     the registry makes for it (the engine); each read doesn't.
func TestEveryNativeSendWaitsUnderEveryName(t *testing.T) {
	asOperator(t)
	t.Setenv(hilsend.EnvAgentContext, "coder")
	r := Build(nil)
	db := sendsDB(t)

	names := map[string][]string{} // canonical type → the names it runs under
	for _, typ := range r.Types() {
		names[typ] = []string{typ}
	}
	aliases := r.Aliases()
	for _, alias := range sortedKeys(aliases) {
		to := aliases[alias]
		if strings.HasPrefix(to, "local-") {
			continue // a legacy ~/.monoagent/actions folder's browser automation
		}
		if got := workflow.CanonicalNodeType(alias); got != to {
			t.Errorf("alias %s runs as %s, but the send gate classifies it as %s", alias, to, got)
		}
		names[to] = append(names[to], alias)
	}

	nothing := map[string]bool{}
	for _, typ := range sendsNothing {
		nothing[typ] = true
	}
	for _, typ := range r.Types() {
		if !strings.HasPrefix(typ, "comm.") && !strings.HasPrefix(typ, "service.") {
			continue
		}
		if _, sends := nativeSends[typ]; sends == nothing[typ] {
			t.Errorf("%s: review it into nativeSends (with nativeReads) or sendsNothing", typ)
		}
	}

	held := func(name string, config map[string]interface{}) bool {
		out, handled, err := hilsend.Guard(sendsCtx(db), name, workflow.NodeInput{}, config)
		return handled && err == nil && len(out) == 1 && out[0].Items[0].JSON["hil_id"] != nil
	}
	cards := 0
	for _, typ := range sortedKeys(nativeSends) {
		if !r.Has(typ) {
			t.Errorf("nativeSends lists %s, which Build doesn't register", typ)
			continue
		}
		reviewed := map[string]bool{}
		for _, op := range append(append([]string{}, nativeSends[typ]...), nativeReads[typ]...) {
			reviewed[op] = true
		}
		for _, name := range names[typ] {
			for _, op := range schemaOps(t, name) {
				if !reviewed[op] {
					t.Errorf("%s: operation %q (%s's schema) is not reviewed: a send (nativeSends) or not (nativeReads)", typ, op, name)
				}
			}
			factory, _ := r.Get(name)
			callers := map[string]string{"node run": name, "the engine": factory().Type()}
			for caller, asked := range callers {
				for _, op := range nativeSends[typ] {
					if !held(asked, sendConfig(op)) {
						t.Errorf("%s %q, asked by %s as %s: an agent's send went out without a card", name, op, caller, asked)
					}
					cards++
				}
				for _, op := range nativeReads[typ] {
					if held(asked, map[string]interface{}{"operation": op}) {
						t.Errorf("%s %q, asked by %s as %s: a read was held as a send", name, op, caller, asked)
					}
				}
			}
		}
	}
	if cards < 120 {
		t.Errorf("only %d sends checked: lost the aliases?", cards)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
