package httpnodes

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/secrets"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Under the owner's agent access a secret read by an unapproved run goes
// only to a host the grant names; a request that carries none is not held
// to it (review of #943, H3).
func TestAgentAccessBindsASecretToItsHosts(t *testing.T) {
	keyring.MockInit()
	db, err := storage.NewDatabase(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	base := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db.DB), "default")
	if _, err := secrets.Add(base, db.DB, "default", "secret", "vs", map[string]string{"secret": "vsecret-777"}, "", "", ""); err != nil {
		t.Fatal(err)
	}
	rec := newRecorder(t, nil)
	cfg := func(body string) map[string]interface{} {
		return map[string]interface{}{"url": rec.URL + "/r", "method": "POST", "body": body}
	}
	run := func(grant secrets.AccessGrant, body string) error {
		if err := secrets.SetAgentAccessGrant(base, db.DB, "default", grant); err != nil {
			t.Fatal(err)
		}
		ctx := secrets.AllowResolving(base, db.DB, "default", secrets.AccessOrigin{WorkflowID: "wf"})
		outs, err := (&RequestNode{}).Execute(ctx, workflow.NodeInput{Items: []workflow.Item{workflow.NewItem(map[string]interface{}{})}}, cfg(body))
		if err != nil {
			return err
		}
		// A failed request is routed to the error output with its message.
		for _, o := range outs {
			if o.Handle == "error" {
				for _, it := range o.Items {
					if msg, _ := it.JSON["error"].(string); msg != "" {
						return errors.New(msg)
					}
				}
			}
		}
		return nil
	}

	err = run(secrets.AccessGrant{Hosts: []string{"api.example.com"}}, "@secret:vs")
	if err == nil || !strings.Contains(err.Error(), secrets.ErrHostNotAllowed.Error()) {
		t.Fatalf("a secret to a host the grant does not name: %v", err)
	}
	rec.mu.Lock()
	n := len(rec.reqs)
	rec.mu.Unlock()
	if n != 0 {
		t.Fatalf("the request was sent: %d", n)
	}
	if err := run(secrets.AccessGrant{Hosts: []string{"api.example.com"}}, "no secret here"); err != nil {
		t.Errorf("a request with no secret was held to the hosts: %v", err)
	}
	if err := run(secrets.AccessGrant{Hosts: []string{"127.0.0.1"}}, "@secret:vs"); err != nil {
		t.Fatalf("a secret to a granted host: %v", err)
	}
	if got := rec.last(t).Body; got != "vsecret-777" {
		t.Errorf("the granted host received %q", got)
	}
	if err := run(secrets.AccessGrant{AnyHost: true}, "@secret:vs"); err != nil {
		t.Errorf("--any-host: %v", err)
	}
	if err := run(secrets.AccessGrant{Hosts: []string{"127.0.0.1"}}, "@secret:vs"); err != nil {
		t.Errorf("again: %v", err)
	}
}

// A proxy is a second destination: a secret may not ride a request through
// a proxy the grant does not name, nor with certificate checks off (review
// of #944, A1).
func TestAgentAccessRefusesProxyAndInsecureForASecret(t *testing.T) {
	keyring.MockInit()
	db, err := storage.NewDatabase(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	base := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db.DB), "default")
	if _, err := secrets.Add(base, db.DB, "default", "secret", "vs", map[string]string{"secret": "vsecret-777"}, "", "", ""); err != nil {
		t.Fatal(err)
	}
	dest := newRecorder(t, nil)
	evil := newRecorder(t, nil)
	if err := secrets.SetAgentAccessGrant(base, db.DB, "default", secrets.AccessGrant{Hosts: []string{"127.0.0.1"}}); err != nil {
		t.Fatal(err)
	}
	run := func(extra map[string]interface{}, body string) error {
		ctx := secrets.AllowResolving(base, db.DB, "default", secrets.AccessOrigin{WorkflowID: "wf"})
		cfg := map[string]interface{}{"url": dest.URL + "/r", "method": "POST", "body": body}
		for k, v := range extra {
			cfg[k] = v
		}
		outs, err := (&RequestNode{}).Execute(ctx, workflow.NodeInput{Items: []workflow.Item{workflow.NewItem(map[string]interface{}{})}}, cfg)
		if err != nil {
			return err
		}
		for _, o := range outs {
			if o.Handle == "error" {
				for _, it := range o.Items {
					if msg, _ := it.JSON["error"].(string); msg != "" {
						return errors.New(msg)
					}
				}
			}
		}
		return nil
	}
	err = run(map[string]interface{}{"proxy": "http://localhost:" + evil.URL[strings.LastIndex(evil.URL, ":")+1:]}, "@secret:vs")
	if err == nil || !strings.Contains(err.Error(), secrets.ErrHostNotAllowed.Error()) {
		t.Fatalf("a secret through a proxy the grant does not name: %v", err)
	}
	if n := len(evil.reqs) + len(dest.reqs); n != 0 {
		t.Fatalf("something was sent: %d", n)
	}
	err = run(map[string]interface{}{"allow_unauthorized_certs": true}, "@secret:vs")
	if err == nil || !strings.Contains(err.Error(), secrets.ErrHostNotAllowed.Error()) {
		t.Fatalf("a secret with certificate checks off: %v", err)
	}
	if err := run(map[string]interface{}{"proxy": "http://localhost:1", "allow_unauthorized_certs": true}, "no secret"); err != nil && strings.Contains(err.Error(), secrets.ErrHostNotAllowed.Error()) {
		t.Errorf("a request with no secret was held: %v", err)
	}
}
