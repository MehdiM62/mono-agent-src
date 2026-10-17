package hil

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/operator"
	"github.com/monoes/mono-agent/internal/workflow"
)

// A Human-in-Loop item's data can hold a reply link the run sends once the
// item is approved (here the run's resume URL): `hil list` from a marked
// process hides its token and the user's terminal shows it. An approval
// whose --data holds the placeholder is refused, so it can't replace the
// link; one that leaves the field out goes through.
func TestHILListHidesReplyTokensFromAMarkedProcess(t *testing.T) {
	t.Setenv("MONOAGENT_PUBLIC_URL", "http://127.0.0.1:19724")
	for _, k := range operator.MarkerEnv() {
		t.Setenv(k, "")
	}
	cfg := &testConfig{DBPath: newHILTestDB(t), JSONOutput: true, ProfileID: "default"}
	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	resume, err := workflow.ExecutionResumeURL("ex-r")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO hil_pending (id, execution_id, workflow_id, node_id, node_name, status, readonly_data, editable_data, node_config, profile_id)
		VALUES ('h1', 'ex-r', 'wf-r', 'n', 'Review', 'pending', ?, ?, '{"outbound":false}', 'default')`,
		`{"link":"`+resume+`"}`, `{"body":"Answer at `+resume+`"}`); err != nil {
		t.Fatal(err)
	}

	mine, err := runHIL(t, cfg, "list")
	if err != nil || len(mine) != 1 || mine[0].ReadonlyData["link"] != resume {
		t.Fatalf("the user's terminal: %+v %v", mine, err)
	}
	t.Setenv("MONOAGENT_AI_CALLER", "chat")
	theirs, err := runHIL(t, cfg, "list")
	hidden := "http://127.0.0.1:19724/reply/" + workflow.ReplyTokenHidden
	if err != nil || len(theirs) != 1 || theirs[0].ReadonlyData["link"] != hidden || theirs[0].EditableData["body"] != "Answer at "+hidden {
		t.Fatalf("the chat: %+v %v", theirs, err)
	}

	if _, err := runHIL(t, cfg, "approve", "h1", "--data", `{"body":"Answer at `+hidden+`"}`); exitCodeFor(err) != 3 || !strings.Contains(err.Error(), "leave out the fields") {
		t.Fatalf("approve with the placeholder: %v (exit %d)", err, exitCodeFor(err))
	}
	if _, err := runHIL(t, cfg, "approve", "h1", "--data", `{"note":"ok"}`); err != nil {
		t.Fatalf("approve without it: %v", err)
	}
}
