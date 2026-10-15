package publication

import (
	"context"
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/profiledir"
)

func TestRegisterNeedsAProfile(t *testing.T) {
	base := testStore(t)
	s := NewStore(base.db, "")
	_, err := s.Register(context.Background(), Entry{Platform: "x", URL: "https://example.com/p/1"})
	if !errors.Is(err, profiledir.ErrNoProfile) {
		t.Fatalf("Register(no profile) = %v, want ErrNoProfile", err)
	}
}
