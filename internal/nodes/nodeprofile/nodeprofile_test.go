package nodeprofile

import (
	"bytes"
	"context"
	"log/slog"
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

// The old schema default wrote profile_id "default" into saved workflows; a
// run under another profile ignores that literal (with a warning) instead of
// failing, while any other differing value stays an error.
func TestRequireIgnoresTheLegacyDefaultLiteral(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	ctx := vault.ContextWithProfileID(context.Background(), "P")
	got, err := Require(ctx, "people.save", map[string]interface{}{"profile_id": "default"})
	if err != nil || got != "P" {
		t.Fatalf("legacy default: got %q, %v; want P", got, err)
	}
	if w := buf.String(); !strings.Contains(w, "ignoring legacy profile_id=default") || !strings.Contains(w, "people.save") || !strings.Contains(w, "P") {
		t.Fatalf("warning = %q", w)
	}
	// Run under the profile literally named "default": nothing to warn about.
	buf.Reset()
	dctx := vault.ContextWithProfileID(context.Background(), "default")
	if got, err := Require(dctx, "people.save", map[string]interface{}{"profile_id": "default"}); err != nil || got != "default" || buf.Len() != 0 {
		t.Fatalf("matching default: %q, %v, log %q", got, err, buf.String())
	}
	if _, err := Require(ctx, "people.save", map[string]interface{}{"profile_id": "other"}); err == nil {
		t.Fatal("a differing non-default profile_id must stay an error")
	}
}
