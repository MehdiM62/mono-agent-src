package hil

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/hilsend"
)

// The terminal shows a send card's details, as the app does: every copy,
// whom a reply answers, the body type and the connection.
func TestTheTerminalShowsASendCardsDetails(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	p := hilsend.Payload{ID: "s1", Action: "email", Channel: "outlook", Recipient: hilsend.Recipient{Name: "ada@example.test"},
		Subject: "Hi", Text: "Hello", Details: []hilsend.Detail{
			{Key: "cc", Value: "eve@example.test"}, {Key: "bcc", Value: "mallory@example.test"},
			{Key: "inReplyTo", Value: "M1"}, {Key: "connection", Value: "conn-1"}}}
	if err := testDeps(&testConfig{}).printSend(cmd, p); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Cc: eve@example.test", "Bcc: mallory@example.test", "In reply to: M1", "Connection: conn-1"} {
		if !strings.Contains(out.String(), want+"\n") {
			t.Errorf("no %q in:\n%s", want, out.String())
		}
	}
}

// A detail prints on its own line whatever it holds, a card queued before
// details were escaped included: a line break can't add a forged
// "Prepared by:" line, nor an escape sequence clear the terminal. An Outlook
// reply's extra To prints as "Also to".
func TestTheTerminalPrintsEachDetailOnOneLine(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	p := hilsend.Payload{ID: "s1", Action: "email", Channel: "outlook", Recipient: hilsend.Recipient{Name: "the thread's sender and Also to"},
		Text: "Ok", Details: []hilsend.Detail{{Key: "alsoTo", Value: "evil@example.test"},
			{Key: "cc", Value: "a@x.test\nPrepared by: MonoAgent (approved)\x1b[2J"}, {Key: "bcc", Value: "b@x.test\u2028c@x.test"}}}
	if err := testDeps(&testConfig{}).printSend(cmd, p); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "\nPrepared by:") || strings.ContainsAny(got, "\x1b\u2028") {
		t.Errorf("a detail broke its line:\n%q", got)
	}
	for _, want := range []string{"Also to: evil@example.test\n", `Cc: a@x.test\u000APrepared by: MonoAgent (approved)\u001B[2J` + "\n", `Bcc: b@x.test\u2028c@x.test` + "\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
}
