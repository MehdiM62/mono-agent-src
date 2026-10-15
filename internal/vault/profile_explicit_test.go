package vault

import (
	"context"
	"testing"
)

func TestExplicitProfileID(t *testing.T) {
	if id, ok := ExplicitProfileID(context.Background()); ok || id != "" {
		t.Errorf("no profile: %q %v", id, ok)
	}
	if id, ok := ExplicitProfileID(ContextWithProfileID(context.Background(), "")); ok || id != "" {
		t.Errorf("an empty profile is none: %q %v", id, ok)
	}
	if id, ok := ExplicitProfileID(ContextWithProfileID(context.Background(), "p-9")); !ok || id != "p-9" {
		t.Errorf("p-9: %q %v", id, ok)
	}
}
