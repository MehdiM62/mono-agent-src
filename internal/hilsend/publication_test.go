package hilsend

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
)

// sentEmail queues an email as an agent and has the user approve it; the
// send's node returns result. It returns the item's id.
func sentEmail(t *testing.T, db *sql.DB, result string) string {
	t.Helper()
	id := queueEmail(t, db)
	run := func(context.Context, Run) (json.RawMessage, error) { return json.RawMessage(result), nil }
	if _, err := Resolve(context.Background(), Store{db, "p1"}, id, true, Edit{}, false, run); err != nil {
		t.Fatal(err)
	}
	return id
}

// A publication from whatever is not the operator is the one a send the
// user approved made: that send is sent, on the publication's platform,
// returned its remote id where both have one, and registers one publication
// only. Nothing is asked while approvals of sends are off.
func TestClaimPublicationApproval(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	ctx := context.Background()
	pending := queueEmail(t, db)
	sent := sentEmail(t, db, `{"main":[{"json":{"message_id":"m-42"}}]}`)
	platform := func(id string) string {
		p, err := (Store{db, "p1"}).Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if p.Account.Platform != "" {
			return p.Account.Platform
		}
		return p.Channel
	}(sent)

	for _, approval := range []string{"", pending, "missing"} {
		if _, err := ClaimPublicationApproval(ctx, db, "p1", approval, platform, ""); !errors.Is(err, ErrPublicationNeedsApproval) {
			t.Errorf("approval %q, not sent: %v", approval, err)
		}
	}
	if _, err := ClaimPublicationApproval(ctx, db, "p2", sent, platform, ""); !errors.Is(err, ErrPublicationNeedsApproval) {
		t.Errorf("another profile's send: %v", err)
	}
	var mismatch *PublicationMismatchError
	if _, err := ClaimPublicationApproval(ctx, db, "p1", sent, "x", ""); !errors.As(err, &mismatch) || mismatch.What != "platform" {
		t.Errorf("another platform: %v", err)
	}
	if _, err := ClaimPublicationApproval(ctx, db, "p1", sent, platform, "m-7"); !errors.As(err, &mismatch) || mismatch.What != "remote id" {
		t.Errorf("another remote id: %v", err)
	}
	claim, err := ClaimPublicationApproval(ctx, db, "p1", sent, platform, "m-42")
	if err != nil || claim == nil {
		t.Fatalf("the send's own publication: %v", err)
	}
	if _, err := ClaimPublicationApproval(ctx, db, "p1", sent, platform, "m-42"); !errors.Is(err, ErrPublicationApprovalUsed) {
		t.Errorf("the approval again: %v", err)
	}
	// A registration that failed gives it back.
	claim.Release(ctx, db)
	if claim, err = ClaimPublicationApproval(ctx, db, "p1", sent, platform, ""); err != nil || claim == nil {
		t.Errorf("after a release: %v", err)
	}
	if p, err := (Store{db, "p1"}).Get(ctx, sent); err != nil || p.State != StateSent {
		t.Errorf("the claimed send: %+v %v", p, err)
	}

	if _, err := db.Exec(`INSERT INTO configs (name, config_data) VALUES ('coder_mode', '{"sendApproval":false}')`); err != nil {
		t.Fatal(err)
	}
	if claim, err := ClaimPublicationApproval(ctx, db, "p1", "", "anything", ""); err != nil || claim != nil {
		t.Errorf("approvals of sends off: %v %v", claim, err)
	}
}

func TestSentRemoteID(t *testing.T) {
	for result, want := range map[string]string{
		`{"main":[{"json":{"remote_id":"r","id":"i"}}]}`:   "r",
		`{"main":[{"json":{"id":12345}}]}`:                 "12345",
		`{"main":[{"json":{}},{"json":{"uri":"at://x"}}]}`: "at://x",
		`{"error":[{"json":{"id":"e"}}]}`:                  "",
		`"plain text"`:                                     "",
	} {
		var v any
		_ = json.Unmarshal([]byte(result), &v)
		if got := sentRemoteID(v); got != want {
			t.Errorf("%s: %q, want %q", result, got, want)
		}
	}
}
