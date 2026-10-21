package agenttokens

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/testdb"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(testdb.Open(t).DB)
}

func TestScopeLadder(t *testing.T) {
	cases := []struct {
		have, need Scope
		want       bool
	}{
		{ScopeRead, ScopeRead, true}, {ScopeRead, ScopeWrite, false}, {ScopeRead, ScopeRun, false}, {ScopeRead, ScopeFull, false},
		{ScopeWrite, ScopeRead, true}, {ScopeWrite, ScopeWrite, true}, {ScopeWrite, ScopeRun, false}, {ScopeWrite, ScopeFull, false},
		{ScopeRun, ScopeWrite, true}, {ScopeRun, ScopeRun, true}, {ScopeRun, ScopeFull, false},
		{ScopeFull, ScopeFull, true}, {ScopeFull, ScopeRead, true},
		{"", ScopeRead, false}, {"admin", ScopeRead, false}, {ScopeFull, "", false},
	}
	for _, c := range cases {
		if got := c.have.Allows(c.need); got != c.want {
			t.Errorf("%q.Allows(%q) = %v, want %v", c.have, c.need, got, c.want)
		}
	}
}

func TestCreateNeverIssuesFullOrUnknownScope(t *testing.T) {
	s := newStore(t)
	for _, sc := range []Scope{ScopeFull, "admin", ""} {
		if _, _, err := s.Create(context.Background(), "default", "x", sc, time.Hour); err == nil {
			t.Errorf("scope %q was issued", sc)
		}
	}
}

func TestCreateStoresOnlyTheHashAndVerifies(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	tok, secret, err := s.Create(ctx, "default", "ci", ScopeRead, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, Prefix) || tok.Scope != ScopeRead || tok.ExpiresAt == nil {
		t.Fatalf("token %+v secret %q", tok, secret)
	}
	var dump string
	rows, _ := s.db.Query(`SELECT id||name||scope||prefix||token_hash FROM agent_tokens`)
	for rows.Next() {
		var v string
		_ = rows.Scan(&v)
		dump += v
	}
	rows.Close()
	if strings.Contains(dump, secret) {
		t.Fatal("the token is stored in the clear")
	}
	if !strings.Contains(dump, Hash(secret)) {
		t.Fatal("the hash is not stored")
	}
	got, err := s.Verify(ctx, secret)
	if err != nil || got.ID != tok.ID {
		t.Fatalf("Verify = %+v, %v", got, err)
	}
	for _, bad := range []string{"", "mat-nope", secret + "x", secret[:len(secret)-1]} {
		if _, err := s.Verify(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("Verify(%q) = %v, want ErrInvalid", bad, err)
		}
	}
}

func TestExpiredAndRevokedTokensAreInvalid(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })
	_, exp, _ := s.Create(ctx, "default", "short", ScopeWrite, time.Hour)
	_, rev, _ := s.Create(ctx, "default", "gone", ScopeWrite, 0)
	_, forever, _ := s.Create(ctx, "default", "forever", ScopeWrite, 0)

	if _, err := s.Verify(ctx, exp); err != nil {
		t.Fatalf("fresh token refused: %v", err)
	}
	now = now.Add(time.Hour) // the expiry instant itself is already over
	if _, err := s.Verify(ctx, exp); !errors.Is(err, ErrInvalid) {
		t.Errorf("expired token: %v", err)
	}
	if _, err := s.Verify(ctx, forever); err != nil {
		t.Errorf("a token with no expiry was refused: %v", err)
	}
	if _, err := s.Revoke(ctx, "default", "gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(ctx, rev); !errors.Is(err, ErrInvalid) {
		t.Errorf("revoked token: %v", err)
	}
	// revoking twice is fine; the name is free again
	if _, err := s.Revoke(ctx, "default", "gone"); err == nil {
		t.Log("second revoke by name: not found (name resolves only unrevoked)")
	}
	if _, _, err := s.Create(ctx, "default", "gone", ScopeRead, 0); err != nil {
		t.Errorf("a revoked token's name was not reusable: %v", err)
	}
}

func TestNamesAreValidatedAndUniquePerProfile(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	for _, n := range []string{"", " x", "a b", "mat-abc", "xMAT-y", strings.Repeat("a", 65)} {
		if _, _, err := s.Create(ctx, "default", n, ScopeRead, 0); !errors.Is(err, ErrInvalidName) {
			t.Errorf("name %q: %v", n, err)
		}
	}
	if _, _, err := s.Create(ctx, "default", "a", ScopeRead, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Create(ctx, "default", "a", ScopeRead, 0); !errors.Is(err, ErrNameTaken) {
		t.Errorf("duplicate: %v", err)
	}
	if _, _, err := s.Create(ctx, "other", "a", ScopeRead, 0); err != nil {
		t.Errorf("same name in another profile: %v", err)
	}
}

func TestAuditHoldsTheNameNeverTheToken(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	tok, secret, _ := s.Create(ctx, "default", "bot", ScopeRead, 0)
	if err := s.RecordUse(ctx, tok, "GET", "/profiles/default/workflows", 200); err != nil {
		t.Fatal(err)
	}
	uses, err := s.Audit(ctx, tok, 10)
	if err != nil || len(uses) != 1 || uses[0].TokenName != "bot" || uses[0].Status != 200 || uses[0].Method != "GET" {
		t.Fatalf("uses = %+v, %v", uses, err)
	}
	var dump string
	rows, _ := s.db.Query(`SELECT token_id||token_name||profile_id||method||path FROM agent_token_audit`)
	for rows.Next() {
		var v string
		_ = rows.Scan(&v)
		dump += v
	}
	rows.Close()
	if strings.Contains(dump, secret) {
		t.Fatal("audit holds the token")
	}
}
