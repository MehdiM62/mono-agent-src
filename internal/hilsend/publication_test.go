package hilsend

import (
	"context"
	"errors"
	"testing"
)

// A publication from whatever is not the operator names a send that was
// approved and sent, unless approvals of sends are off.
func TestPublicationApproved(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	ctx := context.Background()
	id := queueEmail(t, db)
	for _, approval := range []string{"", id, "missing"} {
		if err := PublicationApproved(ctx, db, "p1", approval); !errors.Is(err, ErrPublicationNeedsApproval) {
			t.Errorf("approval %q before the send went out: %v", approval, err)
		}
	}
	f := &fakeRunner{}
	if _, err := Resolve(ctx, Store{db, "p1"}, id, true, Edit{}, false, f.run); err != nil {
		t.Fatal(err)
	}
	if err := PublicationApproved(ctx, db, "p1", id); err != nil {
		t.Errorf("a sent send: %v", err)
	}
	if err := PublicationApproved(ctx, db, "p2", id); !errors.Is(err, ErrPublicationNeedsApproval) {
		t.Errorf("another profile's send: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO configs (name, config_data) VALUES ('coder_mode', '{"sendApproval":false}')`); err != nil {
		t.Fatal(err)
	}
	if err := PublicationApproved(ctx, db, "p1", ""); err != nil {
		t.Errorf("approvals of sends off: %v", err)
	}
}
