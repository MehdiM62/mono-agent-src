package apipkg

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// sendWords mark an op name that sends to people; readVerbs, as an op's
// first word, one that only reads or files things.
var (
	sendWords = map[string]bool{"send": true, "message": true, "messages": true, "sms": true, "mms": true, "email": true,
		"mail": true, "notification": true, "notify": true, "publish": true, "reply": true, "comment": true, "post": true,
		"tweet": true, "push": true, "invite": true, "reinvite": true, "call": true, "voice": true, "campaign": true,
		"broadcast": true, "dm": true, "share": true}
	readVerbs = map[string]bool{"get": true, "getall": true, "list": true, "search": true, "count": true, "check": true,
		"analyze": true, "predict": true, "mark": true, "move": true, "delete": true, "remove": true, "pause": true,
		"resolve": true, "close": true, "leave": true, "pin": true, "unpin": true, "undo": true, "soft": true, "lock": true,
		"duplicate": true, "replicate": true, "react": true, "like": true, "retweet": true, "rate": true, "execute": true,
		"status": true, "track": true}
	camelHump  = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	nonWordRun = regexp.MustCompile(`[^A-Za-z0-9]+`)
)

func opWords(s string) []string {
	var out []string
	for _, w := range nonWordRun.Split(camelHump.ReplaceAllString(s, "$1 $2"), -1) {
		if w != "" {
			out = append(out, strings.ToLower(w))
		}
	}
	return out
}

// The catalog lint: an op whose name says it sends (a send word in its
// resource or operation, a write method, and not a read verb first) must
// say whether it does: "sends": {…}, or "sends": false with a reason. A new
// package cannot leave a send out of the user's approvals by saying nothing.
func TestCatalogSendsAreDecided(t *testing.T) {
	pkgs := catalogPackages(t)
	var undecided []string
	sends := 0
	for _, p := range pkgs {
		for _, o := range p.Operations {
			if o.Sends != nil && !o.Sends.None {
				sends++
			}
			switch strings.ToUpper(o.Method) {
			case "POST", "PUT", "PATCH":
			default:
				continue
			}
			ow := opWords(o.Operation)
			if len(ow) == 0 || readVerbs[ow[0]] {
				continue
			}
			named := false
			for _, w := range append(opWords(o.Resource), ow...) {
				named = named || sendWords[w]
			}
			if named && o.Sends == nil {
				undecided = append(undecided, p.ID+" "+o.Resource+"/"+o.Operation)
			}
		}
	}
	if len(undecided) > 0 {
		t.Errorf("%d op(s) look like sends but say neither \"sends\": {…} nor \"sends\": false with a sends_reason (FORMAT.md §3):\n  %s",
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
	if !SendsSome("app.mailgun") || !SendsSome("comm.slack") || SendsSome("app.aws_sqs") || SendsSome("comm.email_send") {
		t.Error("SendsSome")
	}
}
