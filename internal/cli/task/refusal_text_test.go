package task

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/operator"
)

// A marker's refusal reads as one sentence: the reason, then the hint on an
// inherited marker in parentheses (as operator.Refusal gives it), then what
// only the operator does. The hint used to end in a period that the colon
// after it followed (".:").
func TestAMarkersRefusalReadsAsOneSentence(t *testing.T) {
	t.Parallel()
	db := newTaskTestDB(t)
	ids := seedTaskRows(t, db, taskSeed{title: "captured"})
	setenv(t, "CLAUDECODE", "1")
	why := "CLAUDECODE is set, so an agent is running this command (" + operator.InheritedHintClause() + ")"
	want := why + ": only the operator can approve a task; run it in your own terminal"
	if msg, _ := failedTaskJSON(t, db, "default", 3, "approve", id(ids[0]))["error"].(string); msg != want {
		t.Errorf("approve:\n got %q\nwant %q", msg, want)
	}
	board, _ := failedTaskJSON(t, db, "default", 3, "board")["error"].(string)
	if !strings.HasPrefix(board, why+": only the operator can show the board; run it in your own terminal. The board shows the Inbox") {
		t.Errorf("board: %q", board)
	}
	for _, msg := range []string{want, board} {
		if strings.Contains(msg, ".:") || strings.Contains(msg, "..") {
			t.Errorf("a sentence breaks in %q", msg)
		}
	}
}
