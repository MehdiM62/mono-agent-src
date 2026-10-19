package apipkg

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// The catalog lint (sends_lint_test.go): an op that may send says whether
// it does, "sends": {…} or "sends": false with a reason, so a new package
// cannot leave a send out of the user's approvals by saying nothing.
func TestCatalogSendsAreDecided(t *testing.T) {
	pkgs := catalogPackages(t)
	ids := map[string]bool{}
	sends := 0
	for _, p := range pkgs {
		ids[p.ID] = true
		for _, o := range p.Operations {
			if o.Sends != nil && !o.Sends.None {
				sends++
			}
		}
	}
	for id := range messagingPackages {
		if !ids[id] {
			t.Errorf("messagingPackages names %s, which is not in the catalog", id)
		}
	}
	if undecided := undecidedSends(pkgs); len(undecided) > 0 {
		t.Errorf("%d op(s) may send but say neither \"sends\": {…} nor \"sends\": false with a sends_reason (FORMAT.md §3):\n  %s",
			len(undecided), strings.Join(undecided, "\n  "))
	}
	if sends < 150 {
		t.Errorf("only %d annotated sends in the catalog: did the annotations get lost?", sends)
	}
}

// catalogPackages loads the built-in catalog, failing on any invalid file
// (a sends block naming a field the op lacks, a false without a reason).
func catalogPackages(t *testing.T) []*Package {
	t.Helper()
	names, err := CatalogFiles()
	if err != nil {
		t.Fatal(err)
	}
	var out []*Package
	for _, name := range names {
		b, err := catalogFS.ReadFile("catalog/" + name)
		if err != nil {
			t.Fatal(err)
		}
		p, err := Parse(b, "catalog/"+name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out = append(out, p)
	}
	return out
}

// SendOf answers for an app node's op (its default op when the config names
// none), for an op a package adds to a native node, and for nothing else.
func TestSendOf(t *testing.T) {
	for _, c := range []struct {
		name, nodeType string
		config         map[string]any
		want           *Send
	}{
		{"mailgun", "app.mailgun", map[string]any{"operation": "send"},
			&Send{Action: "email", Channel: "mailgun", To: []string{"toEmail"}, Text: []string{"text", "html"}, Subject: []string{"subject"}}},
		{"mailgun's default op", "app.mailgun", map[string]any{}, &Send{Action: "email", Channel: "mailgun"}},
		{"slack's message/post on comm.slack", "comm.slack", map[string]any{"resource": "message", "operation": "post"},
			&Send{Action: "post", To: []string{"channelId", "user"}, Text: []string{"text"}}},
		{"a native slack op", "comm.slack", map[string]any{"operation": "post_message"}, nil},
		{"a read", "app.mattermost", map[string]any{"resource": "channel", "operation": "search"}, nil},
		{"sends false", "app.aws_sqs", map[string]any{"operation": "sendMessage"}, nil},
		{"sendy without sending", "app.sendy", map[string]any{"resource": "campaign", "operation": "create", "sendCampaign": false}, nil},
		{"sendy sending", "app.sendy", map[string]any{"resource": "campaign", "operation": "create", "sendCampaign": true},
			&Send{Action: "email", Channel: "sendy"}},
		{"not a package", "comm.email_send", map[string]any{}, nil},
		// Zulip's notification bot posts a stream's new name or description
		// in the stream, a description cleared to "" included, and its
		// privacy and posting permissions; raw JSON may set any of them.
		{"zulip renaming a stream", "app.zulip", map[string]any{"resource": "stream", "operation": "update", "streamId": "7",
			"additionalFields": map[string]any{"newName": "launch"}}, &Send{Action: "post", Channel: "zulip", To: []string{"streamId"}}},
		{"zulip updating a stream with raw JSON", "app.zulip", map[string]any{"resource": "stream", "operation": "update", "streamId": "7",
			"jsonParameters": true, "additionalFieldsJson": `{"description":"x"}`}, &Send{Action: "post", Channel: "zulip"}},
		{"zulip making a stream private", "app.zulip", map[string]any{"resource": "stream", "operation": "update", "streamId": "7",
			"additionalFields": map[string]any{"isPrivate": true}}, &Send{Action: "post", Channel: "zulip", To: []string{"streamId"}}},
		{"zulip clearing a stream's description", "app.zulip", map[string]any{"resource": "stream", "operation": "update", "streamId": "7",
			"additionalFields": map[string]any{"description": ""}}, &Send{Action: "post", Channel: "zulip"}},
		{"zulip changing who may post", "app.zulip", map[string]any{"resource": "stream", "operation": "update", "streamId": "7",
			"additionalFields": map[string]any{"streamPostPolicy": 2}}, &Send{Action: "post", Channel: "zulip"}},
		{"zulip updating nothing", "app.zulip", map[string]any{"resource": "stream", "operation": "update", "streamId": "7",
			"additionalFields": map[string]any{"isPrivate": false}}, nil},
		// A Salesforce flow may email what its inputs say: it waits when
		// given any; a flow invoked with none runs as built.
		{"salesforce invoking a flow", "service.salesforce", map[string]any{"resource": "flow", "operation": "invoke", "apiName": "Notify"}, nil},
		{"salesforce invoking a flow with raw variables", "service.salesforce", map[string]any{"resource": "flow", "operation": "invoke",
			"apiName": "Notify", "jsonParameters": true, "variablesJson": `{"body":"x"}`},
			&Send{Action: "email", To: []string{"apiName"}}},
		{"salesforce invoking a flow with variables", "service.salesforce", map[string]any{"resource": "flow", "operation": "invoke",
			"apiName": "Notify", "variablesUi": map[string]any{"variablesValues": []any{map[string]any{"name": "body", "value": "x"}}}},
			&Send{Action: "email"}},
	} {
		got, ok := SendOf(c.nodeType, c.config)
		if c.want == nil {
			if ok {
				t.Errorf("%s: %+v, want no send", c.name, got)
			}
			continue
		}
		if !ok || got.Action != c.want.Action || got.Channel != c.want.Channel ||
			(c.want.To != nil && !reflect.DeepEqual(got.To, c.want.To)) ||
			(c.want.Text != nil && !reflect.DeepEqual(got.Text, c.want.Text)) ||
			(c.want.Subject != nil && !reflect.DeepEqual(got.Subject, c.want.Subject)) {
			t.Errorf("%s: (%+v, %v), want %+v", c.name, got, ok, *c.want)
		}
		if strings.HasPrefix(c.nodeType, NodeTypePrefix) && len(got.PerItem) == 0 {
			t.Errorf("%s: an app node resolves its params per item, PerItem is empty", c.name)
		}
	}
	// A send whose words are settings or a flow's inputs lists its params
	// on the card, read-only, in place of a message.
	for _, c := range []struct {
		nodeType string
		config   map[string]any
		op       string
		show     []string
	}{
		{"app.zulip", map[string]any{"resource": "stream", "operation": "update", "streamId": "7", "additionalFields": map[string]any{"isPrivate": true}},
			"zulip stream/update", []string{"additionalFields.description", "additionalFields.newName", "additionalFields.isPrivate",
				"additionalFields.historyPublicToSubscribers", "additionalFields.isAnnouncementOnly", "additionalFields.streamPostPolicy", "additionalFieldsJson"}},
		{"service.salesforce", map[string]any{"resource": "flow", "operation": "invoke", "apiName": "Notify", "jsonParameters": true, "variablesJson": "{}"},
			"salesforce flow/invoke", []string{"variablesUi.variablesValues", "variablesJson"}},
	} {
		got, ok := SendOf(c.nodeType, c.config)
		if !ok || got.Op != c.op || !reflect.DeepEqual(got.Show, c.show) || got.Undeclared || got.Text != nil {
			t.Errorf("%s: %+v, want op %q showing %v", c.nodeType, got, c.op, c.show)
		}
	}
	if !SendsSome("app.mailgun") || !SendsSome("comm.slack") || SendsSome("app.aws_sqs") || SendsSome("comm.email_send") {
		t.Error("SendsSome")
	}
}

// The reasons read as English: "an account", not "a account" (a user, a
// URL and a uProc lookup say "you"), and "companies", not "companys".
func TestSendsReasonsReadWell(t *testing.T) {
	bad := regexp.MustCompile(`\ba [aeioAEIO]\w*|\ba u[^sPR]\w*|\b\w+[^aeiou ]ys\b`)
	for _, p := range catalogPackages(t) {
		for _, o := range p.Operations {
			if m := bad.FindString(o.SendsReason); m != "" {
				t.Errorf("%s %s/%s: %q in %q", p.ID, o.Resource, o.Operation, m, o.SendsReason)
			}
		}
	}
}
