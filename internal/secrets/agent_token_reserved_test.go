package secrets

import (
	"context"
	"errors"
	"testing"
)

// agent-token-* entries hold scoped agent tokens: only the operator-gated token
// commands (a ctx that allows reserved writes) write them, and a run under the
// owner's agent access never reads one, even with access on for every secret.
func TestAgentTokenEntriesAreReserved(t *testing.T) {
	db := newSecretsTestDB(t)
	ctx := context.Background()
	allowed := AllowReservedWrite(ctx)
	id, err := Add(allowed, db.DB, "default", "secret", "agent-token-ci", map[string]string{"token": "mat-abcdefgh"}, "", "", "")
	if err != nil {
		t.Fatalf("the token command's own write: %v", err)
	}
	other, err := Add(ctx, db.DB, "default", "secret", "openai", map[string]string{"secret": "sk-1"}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	renamed := " Agent-Token-x "
	for what, write := range map[string]func() error{
		"add one": func() error {
			_, err := Add(ctx, db.DB, "default", "secret", "agent-token-evil", map[string]string{"token": "x"}, "", "", "")
			return err
		},
		"rename another to one": func() error { return Update(ctx, db.DB, "default", other, &renamed, nil, nil, nil, nil) },
		"change one": func() error {
			return Update(ctx, db.DB, "default", id, nil, nil, nil, nil, map[string]string{"token": "x"})
		},
		"delete one": func() error { return Delete(ctx, db.DB, "default", id) },
	} {
		if err := write(); !errors.Is(err, ErrReservedEntry) {
			t.Errorf("%s: %v, want ErrReservedEntry", what, err)
		}
	}

	if err := SetAgentAccess(ctx, db.DB, "default", true); err != nil {
		t.Fatal(err)
	}
	run := AllowResolving(ctx, db.DB, "default", AccessOrigin{WorkflowID: "wf"})
	if _, err := Resolve(run, db.DB, "default", "@secret:agent-token-ci"); err == nil {
		t.Error("an agent-access run read an agent token")
	}
	if v, err := Resolve(run, db.DB, "default", "@secret:openai"); err != nil || v != "sk-1" {
		t.Errorf("another secret under agent access: %q, %v", v, err)
	}
}
