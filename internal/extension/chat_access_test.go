package extension

import (
	"slices"
	"strings"
	"testing"
)

// The panel's chat is scoped: its conversations are created scoped, and
// every turn, with or without a page attached, carries the one page-read
// tools mode. A conversation the operator raised refuses that mode, so a
// page can neither widen a scoped chat nor reach a higher level through it.
func TestPanelChatStaysScoped(t *testing.T) {
	create := chatCreateArgv("p", "claude", "")
	if !slices.Contains(create, "--access=scoped") {
		t.Errorf("create argv does not name the scoped level: %v", create)
	}
	for _, attach := range [][]string{nil, {"/x/page.png"}} {
		argv := chatTurnArgv(ChatTurnSpec{Profile: "p", Conversation: "c", Turn: "t", Instance: "i", Attach: attach})
		if !slices.Contains(argv, "--tools="+chatToolsMode) || strings.Contains(strings.Join(argv, " "), "--access") {
			t.Errorf("turn argv (attach %v) = %v", attach, argv)
		}
	}
}
