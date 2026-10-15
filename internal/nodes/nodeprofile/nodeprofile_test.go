package nodeprofile

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/vault"
)

func TestRequire(t *testing.T) {
	withA := vault.ContextWithProfileID(context.Background(), "A")

	got, err := Require(withA, "people.save", nil)
	if err != nil || got != "A" {
		t.Fatalf("unset config: got %q, %v; want A", got, err)
	}
	got, err = Require(withA, "people.save", map[string]interface{}{"profile_id": "A"})
	if err != nil || got != "A" {
		t.Fatalf("matching config: got %q, %v; want A", got, err)
	}
	_, err = Require(withA, "people.save", map[string]interface{}{"profile_id": "B"})
	if err == nil || !strings.Contains(err.Error(), "people.save") || !strings.Contains(err.Error(), "B") {
		t.Fatalf("mismatched config: got %v", err)
	}
	_, err = Require(context.Background(), "people.save", nil)
	if err == nil || !strings.Contains(err.Error(), "people.save ran without a profile") {
		t.Fatalf("no ctx profile: got %v", err)
	}
	_, err = Require(context.Background(), "people.save", map[string]interface{}{"profile_id": "B"})
	if err == nil {
		t.Fatal("config profile with no ctx profile must error")
	}
}
