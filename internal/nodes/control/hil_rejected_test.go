package control

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// wireRejected creates workflow_connections with a `rejected` edge from
// the test HIL node (w1/n1) when wired is true.
func wireRejected(t *testing.T, db *sql.DB, wired bool) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE workflow_connections (id TEXT PRIMARY KEY, workflow_id TEXT, source_node_id TEXT,
		source_handle TEXT DEFAULT 'main', target_node_id TEXT, target_handle TEXT DEFAULT 'main', position INTEGER DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO workflow_connections VALUES ('c1','w1','n1','main','ok','main',0)`); err != nil {
		t.Fatal(err)
	}
	if wired {
		if _, err := db.Exec(`INSERT INTO workflow_connections VALUES ('c2','w1','n1','rejected','no','main',0)`); err != nil {
			t.Fatal(err)
		}
	}
}

func twoItemInput() workflow.NodeInput {
	in := hilInput()
	in.Items = []workflow.Item{
		workflow.NewItem(map[string]interface{}{"caption": "first"}),
		workflow.NewItem(map[string]interface{}{"caption": "second"}),
	}
	return in
}

// decide pauses the node once (creating its rows) and then sets the
// statuses of the rows in insertion order.
func decide(t *testing.T, ctx context.Context, db *sql.DB, in workflow.NodeInput, statuses ...string) {
	t.Helper()
	n := &HumanInLoopNode{}
	if _, err := n.Execute(ctx, in, map[string]interface{}{}); !errors.Is(err, workflow.ErrNodePaused) {
		t.Fatalf("first run = %v, want ErrNodePaused", err)
	}
	rows, err := db.Query(`SELECT id FROM hil_pending ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for i, st := range statuses {
		if _, err := db.Exec(`UPDATE hil_pending SET status = ? WHERE id = ?`, st, ids[i]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHIL_RejectedEdgeRoutesRejectedItems(t *testing.T) {
	db := newHILTestDB(t)
	wireRejected(t, db, true)
	ctx := vault.ContextWithDB(context.Background(), db)
	in := twoItemInput()
	decide(t, ctx, db, in, "rejected", "approved")

	out, err := (&HumanInLoopNode{}).Execute(ctx, in, map[string]interface{}{})
	if err != nil {
		t.Fatalf("with a rejected edge the node must succeed, got %v", err)
	}
	got := map[string][]string{}
	for _, o := range out {
		for _, it := range o.Items {
			got[o.Handle] = append(got[o.Handle], it.JSON["caption"].(string))
		}
	}
	if len(got["main"]) != 1 || got["main"][0] != "second" || len(got["rejected"]) != 1 || got["rejected"][0] != "first" {
		t.Fatalf("outputs = %v", got)
	}
}

// With a rejected edge the node waits for every decision before routing.
func TestHIL_RejectedEdgeWaitsForPending(t *testing.T) {
	db := newHILTestDB(t)
	wireRejected(t, db, true)
	ctx := vault.ContextWithDB(context.Background(), db)
	in := twoItemInput()
	decide(t, ctx, db, in, "rejected")
	if _, err := (&HumanInLoopNode{}).Execute(ctx, in, map[string]interface{}{}); !errors.Is(err, workflow.ErrNodePaused) {
		t.Fatalf("one item still pending: got %v, want ErrNodePaused", err)
	}
}

// Without a rejected edge a rejection fails the node with ErrRejected, so
// the run ends REJECTED.
func TestHIL_RejectWithoutEdgeIsErrRejected(t *testing.T) {
	db := newHILTestDB(t)
	wireRejected(t, db, false)
	ctx := vault.ContextWithDB(context.Background(), db)
	in := twoItemInput()
	decide(t, ctx, db, in, "rejected", "approved")
	_, err := (&HumanInLoopNode{}).Execute(ctx, in, map[string]interface{}{})
	if !errors.Is(err, workflow.ErrRejected) {
		t.Fatalf("got %v, want ErrRejected", err)
	}
}
