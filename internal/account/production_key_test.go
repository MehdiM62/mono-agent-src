package account

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

const productionTokenFixture = "testdata/production-key-token.jwt"

// The pinned keys must verify a token signed by the production private key: one the owner mints once
// for a made-up user (it expires within the hour, cannot be refreshed and is safe to commit). Verified
// at the moment it was issued, it proves the public half in pinnedKeys is the half that matches the
// key monoes.me signs with.
//
// The fixture is owner-supplied. While the gate is dormant its absence skips the test; once an
// enforcement date is set the absence fails it, so a date cannot ship without the proof.
func TestProductionKeyVerifiesAProductionToken(t *testing.T) {
	raw, err := os.ReadFile(productionTokenFixture)
	if errors.Is(err, os.ErrNotExist) {
		if !EnforceDate().IsZero() {
			t.Fatalf("the enforcement date is set but %s is missing: the owner mints it from the production key", productionTokenFixture)
		}
		t.Skipf("%s not supplied yet (owner-gated; the gate is dormant)", productionTokenFixture)
	}
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(raw))
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("the fixture is not a compact JWS")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Iat int64 `json:"iat"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Iat == 0 {
		t.Fatalf("the fixture's payload has no iat: %v", err)
	}
	if _, err := Verify(token, time.Unix(claims.Iat, 0).Add(time.Minute)); err != nil {
		t.Fatalf("the pinned keys do not verify a token signed by the production key: %v", err)
	}
}
