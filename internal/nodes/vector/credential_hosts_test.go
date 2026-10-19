package vector

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/connections"
	"github.com/monoes/mono-agent/internal/testdb"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// A vector.store embedding block's own connection sends its key only to
// the host the connection names, or, when it names none, to the block's
// base_url in the user's own workflow: not to one an AI's step sets.
func TestNestedEmbeddingConnectionKeepsItsHost(t *testing.T) {
	keyring.MockInit()
	db := testdb.Open(t).DB
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db), "default")
	store := connections.NewStore(db)
	named := &connections.Connection{ProfileID: "default", Platform: "openai", Method: connections.MethodAPIKey, Label: "router",
		Data: map[string]interface{}{"api_key": "sk-conn", "base_url": "https://router.example/v1"}}
	bare := &connections.Connection{ProfileID: "default", Platform: "cohere", Method: connections.MethodAPIKey, Label: "cohere",
		Data: map[string]interface{}{"api_key": "co-conn"}}
	for _, c := range []*connections.Connection{named, bare} {
		if err := store.Save(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	ai := workflow.WithAIAuthored(ctx)
	for _, c := range []struct {
		name    string
		ctx     context.Context
		cfg     map[string]interface{}
		refused bool
	}{
		{"the connection's host left to it", ai, map[string]interface{}{"credential_id": named.ID}, false},
		{"the connection's host written in", ctx, map[string]interface{}{"credential_id": named.ID, "base_url": "https://ROUTER.example/v1/"}, false},
		{"another host than the connection's", ctx, map[string]interface{}{"credential_id": named.ID, "url": "https://attacker.test/v1"}, true},
		{"an AI's host, the connection naming none", ai, map[string]interface{}{"credential_id": bare.ID, "base_url": "https://attacker.test"}, true},
		{"the user's host, the connection naming none", ctx, map[string]interface{}{"credential_id": bare.ID, "base_url": "https://cohere.proxy.example"}, false},
	} {
		err := resolveNestedCredential(c.ctx, c.cfg)
		if refused := errors.Is(err, workflow.ErrCredentialDestination); refused != c.refused {
			t.Errorf("%s: %v, want refused %v", c.name, err, c.refused)
		}
	}
}

// A region or location becomes part of the provider's host, so it is a
// name, never a host of its own.
func TestEmbedRegionIsAName(t *testing.T) {
	for _, cfg := range []map[string]interface{}{
		{"provider": "bedrock", "access_key_id": "a", "secret_access_key": "s", "region": "attacker.test/x"},
		{"provider": "vertex", "project_id": "p", "access_token": "t", "location": "attacker.test#"},
		{"provider": "vertex", "project_id": "p", "access_token": "t", "region": "evil.test/"},
	} {
		if _, err := parseEmbedConfig(cfg, ""); err == nil || !strings.Contains(err.Error(), "is not a region name") {
			t.Errorf("%v: %v", cfg, err)
		}
	}
	for _, cfg := range []map[string]interface{}{
		{"provider": "bedrock", "access_key_id": "a", "secret_access_key": "s", "region": "eu-west-1"},
		{"provider": "vertex", "project_id": "p", "access_token": "t", "location": "global"},
	} {
		if _, err := parseEmbedConfig(cfg, ""); err != nil {
			t.Errorf("%v: %v", cfg, err)
		}
	}
}
