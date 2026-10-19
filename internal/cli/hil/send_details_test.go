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
