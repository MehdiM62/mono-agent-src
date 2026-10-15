package jevconf

import (
	"context"
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/profiledir"
)

// With no profile nothing is stored under a shared "jev.default.*" key: writes
// are refused, reads see the surface as off, and a client cannot be built.
func TestNoProfileIsNotDefault(t *testing.T) {
	db := newDB(t)

	if err := SetEnabled(db, "", HIL, true); !errors.Is(err, profiledir.ErrNoProfile) {
		t.Fatalf("SetEnabled(no profile) = %v, want ErrNoProfile", err)
	}
	if err := SetThreshold(db, "", HIL, 0.5); !errors.Is(err, profiledir.ErrNoProfile) {
		t.Fatalf("SetThreshold(no profile) = %v, want ErrNoProfile", err)
	}
	if err := SetEnabled(db, "A", HIL, true); err != nil {
		t.Fatal(err)
	}
	if Enabled(db, "", HIL) {
		t.Fatal("a call with no profile must not see another profile's switch")
	}
	if !Enabled(db, "A", HIL) || Enabled(db, "B", HIL) {
		t.Fatal("enablement must be per profile")
	}
	if _, err := NewClient(context.Background(), db, "", "k", "", HIL); !errors.Is(err, profiledir.ErrNoProfile) {
		t.Fatalf("NewClient(no profile) = %v, want ErrNoProfile", err)
	}
}
