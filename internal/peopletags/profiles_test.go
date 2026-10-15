package peopletags

import (
	"context"
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/profiledir"
)

// Two profiles each own a tag called "lead": the second create succeeds, the
// tags are distinct rows, and within one profile the name still merges.
func TestTagNamesArePerProfile(t *testing.T) {
	ctx := context.Background()
	db := testDB(t) // p1, p2 in "prof"; px in "other"

	a, err := Add(ctx, db, "prof", "p1", "lead", "#ff0000")
	if err != nil {
		t.Fatalf("prof lead: %v", err)
	}
	b, err := Add(ctx, db, "other", "px", "lead", "")
	if err != nil {
		t.Fatalf("second profile's lead must succeed: %v", err)
	}
	if a.ID == b.ID {
		t.Fatalf("two profiles share one tag row %q", a.ID)
	}
	if b.Color != DefaultColor {
		t.Errorf("other profile's lead took prof's colour: %+v", b)
	}
	again, err := Add(ctx, db, "prof", "p2", "LEAD", "")
	if err != nil || again.ID != a.ID {
		t.Fatalf("within a profile the name merges: %+v, %v", again, err)
	}
	if got, _ := List(ctx, db, "other", ""); len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("other profile's tags = %+v", got)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tags(id, name, color, profile_id) VALUES('dup', 'Lead', '#000', 'prof')`); err == nil {
		t.Fatal("the schema must still refuse a duplicate name inside one profile")
	}
}

func TestAddNeedsAProfile(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	if _, err := Add(ctx, db, "", "p1", "lead", ""); !errors.Is(err, profiledir.ErrNoProfile) {
		t.Fatalf("Add with no profile = %v, want ErrNoProfile", err)
	}
}
