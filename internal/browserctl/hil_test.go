package browserctl

import (
	"context"
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/profiledir"
	"github.com/monoes/mono-agent/internal/testdb"
)

func TestSubmitItemLifecycle(t *testing.T) {
	actAsHuman(t)
	t.Setenv("MONOAGENT_AGENT_CONTEXT", "1")
	t.Setenv("MONOAGENT_AGENT_ROLE", "coder")
	t.Setenv("MONOAGENT_CONVERSATION_ID", "conv-9")
	ctx := context.Background()
	db := testdb.Open(t).DB
	form := FormSummary{Action: "https://example.com/post", Method: "post", Submitter: "#send",
		Fields: []FormField{{Name: "msg", Type: "text", Value: "hi"}, {Name: "pw", Type: "password", Value: "•••"}}}
	it, err := CreateSubmitItem(ctx, db, SubmitRequest{Profile: "p1", URL: "https://example.com/new?draft=1", Form: form, Via: "click", TabID: 12, Instance: "inst"})
	if err != nil {
		t.Fatal(err)
	}
	if it.Origin != "https://example.com" || it.RequestedBy.Role != "coder" || it.RequestedBy.ConversationID != "conv-9" {
		t.Fatalf("item = %+v", it)
	}

	var execID, wf, node, nodeName, status, nc string
	if err := db.QueryRow(`SELECT execution_id, workflow_id, node_id, node_name, status, node_config FROM hil_pending WHERE id = ?`, it.ID).
		Scan(&execID, &wf, &node, &nodeName, &status, &nc); err != nil {
		t.Fatal(err)
	}
	if execID != "browser:"+it.ID || wf != SubmitKind || node != SubmitKind || nodeName != "Submit on example.com" || status != "pending" {
		t.Fatalf("row = %s %s %s %q %s", execID, wf, node, nodeName, status)
	}
	if !IsSubmitItem(nc) || hilsend.IsSend(nc) {
		t.Fatalf("node_config %s: submit=%v send=%v", nc, IsSubmitItem(nc), hilsend.IsSend(nc))
	}

	// Another profile does not see it.
	if _, err := GetSubmitItem(ctx, db, "other", it.ID); !errors.Is(err, ErrSubmitNotFound) {
		t.Fatalf("other profile: %v", err)
	}
	// A short id works.
	got, err := GetSubmitItem(ctx, db, "p1", it.ID[:8])
	if err != nil || got.ID != it.ID || got.Form.Submitter != "#send" || got.TabID != 12 || got.Instance != "inst" {
		t.Fatalf("get = %+v, %v", got, err)
	}

	var se *SubmitStateError
	if _, err := ClaimSubmitItem(ctx, db, "p1", it.ID); !errors.As(err, &se) || se.Status != "pending" {
		t.Fatalf("claim before approval: %v", err)
	}
	// What `hil approve` does for the operator.
	if got, err := DecideSubmitItem(ctx, db, "p1", it.ID, true); err != nil || got.Status != "approved" || got.ApprovedBy != ApprovedByOperator {
		t.Fatalf("approve: %+v, %v", got, err)
	}
	if _, err := DecideSubmitItem(ctx, db, "p1", it.ID, false); !errors.As(err, &se) || se.Status != "approved" {
		t.Fatalf("second decision: %v", err)
	}
	if _, err := ClaimSubmitItem(ctx, db, "p1", it.ID); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := ClaimSubmitItem(ctx, db, "p1", it.ID); !errors.As(err, &se) || se.Status != "consumed" {
		t.Fatalf("second claim: %v", err)
	}
	if err := ReleaseSubmitItem(ctx, db, "p1", it.ID); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := ClaimSubmitItem(ctx, db, "p1", it.ID); err != nil {
		t.Fatalf("claim after release: %v", err)
	}
	if err := FinishSubmitItem(ctx, db, "p1", it.ID, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = GetSubmitItem(ctx, db, "p1", it.ID)
	if got.Status != "consumed" || got.SubmittedAt == "" || got.ConsumedAt == "" {
		t.Fatalf("finished item = %+v", got)
	}
}

func TestAnApprovalWithoutTheOperatorsStampCannotBeUsed(t *testing.T) {
	actAsHuman(t)
	ctx := context.Background()
	db := testdb.Open(t).DB
	it, err := CreateSubmitItem(ctx, db, SubmitRequest{Profile: "p1", URL: "https://example.com/", Via: "click",
		RequestedBy: &hilsend.PreparedBy{AgentID: "chat"}})
	if err != nil || it.RequestedBy.AgentID != "chat" {
		t.Fatalf("create: %+v, %v", it, err)
	}
	// What the MCP server's and the HTTP API's hil_approve do: status only.
	if _, err := db.Exec(`UPDATE hil_pending SET status='approved' WHERE id=?`, it.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimSubmitItem(ctx, db, "p1", it.ID); !errors.Is(err, ErrApprovedElsewhere) {
		t.Fatalf("claim of an unstamped approval: %v", err)
	}
	if got, _ := GetSubmitItem(ctx, db, "p1", it.ID); got.Status != "approved" || got.RequestedBy.AgentID != "chat" {
		t.Fatalf("item = %+v", got)
	}
}

func TestRejectedSubmitCannotBeUsed(t *testing.T) {
	actAsHuman(t)
	ctx := context.Background()
	db := testdb.Open(t).DB
	it, err := CreateSubmitItem(ctx, db, SubmitRequest{Profile: "p1", URL: "https://example.com/", Via: "guard", Seq: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := DecideSubmitItem(ctx, db, "p1", it.ID, false); err != nil || got.Status != "rejected" || got.ApprovedBy != "" {
		t.Fatalf("reject: %+v, %v", got, err)
	}
	var se *SubmitStateError
	if _, err := ClaimSubmitItem(ctx, db, "p1", it.ID); !errors.As(err, &se) || se.Status != "rejected" {
		t.Fatalf("claim of a rejected item: %v", err)
	}
}

func TestOrdinaryHILItemIsNotASubmit(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t).DB
	if _, err := db.Exec(`INSERT INTO hil_pending (id, execution_id, workflow_id, node_id, node_name, node_config, profile_id)
		VALUES ('wf-item', 'e1', 'w1', 'n1', 'Review', '{"readonly_fields":[]}', 'p1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := GetSubmitItem(ctx, db, "p1", "wf-item"); !errors.Is(err, ErrNotSubmitItem) {
		t.Fatalf("get workflow item: %v", err)
	}
}

// An item belongs to the profile the caller names: none is refused, never
// taken for "default".
func TestSubmitItemsNeedAProfile(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t).DB
	if _, err := CreateSubmitItem(ctx, db, SubmitRequest{URL: "https://example.com/", Via: "click"}); !errors.Is(err, profiledir.ErrNoProfile) {
		t.Fatalf("create with no profile: %v", err)
	}
	it, err := CreateSubmitItem(ctx, db, SubmitRequest{Profile: "p1", URL: "https://example.com/", Via: "click"})
	if err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"get":     func() error { _, err := GetSubmitItem(ctx, db, "", it.ID); return err }(),
		"decide":  func() error { _, err := DecideSubmitItem(ctx, db, "", it.ID, true); return err }(),
		"claim":   func() error { _, err := ClaimSubmitItem(ctx, db, "", it.ID); return err }(),
		"release": ReleaseSubmitItem(ctx, db, "", it.ID),
		"finish":  FinishSubmitItem(ctx, db, "", it.ID, nil),
		"shot":    setSubmitScreenshot(ctx, db, "", it.ID, "/tmp/x.png"),
		"update":  updateSubmit(ctx, db, "", it.ID, "pending", "approved", submitRecord{Kind: SubmitKind}),
	} {
		if !errors.Is(err, profiledir.ErrNoProfile) {
			t.Errorf("%s with no profile: %v", name, err)
		}
	}
	if got, _ := GetSubmitItem(ctx, db, "p1", it.ID); got.Status != "pending" {
		t.Fatalf("the item changed: %+v", got)
	}
}
