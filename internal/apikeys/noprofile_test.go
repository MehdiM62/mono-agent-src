package apikeys

import (
	"context"
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/profiledir"
)

func TestCreateNeedsAProfile(t *testing.T) {
	s, _ := newTestStore(t)
	if _, _, err := s.Create(context.Background(), "", "ci", false); !errors.Is(err, profiledir.ErrNoProfile) {
		t.Fatalf("Create(no profile) = %v, want ErrNoProfile", err)
	}
}
