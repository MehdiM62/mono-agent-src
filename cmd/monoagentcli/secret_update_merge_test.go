package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/secrets"
	"github.com/monoes/mono-agent/internal/storage"
)

// `secret update --field` replaces the entry's whole field set, and says
// which fields that drops; with --merge it sets the fields given and keeps
// the others. --merge without fields is refused.
func TestSecretUpdateMergesOrSaysWhatItDrops(t *testing.T) {
	account.SetEnforceFromForTest(t, time.Time{})
	keyring.MockInit()
	asOperatorProcess(t)
	home := t.TempDir()
	run := func(args ...string) (string, error) {
		t.Helper()
		_, stderr, err := runCLIIn(t, home, "", append([]string{"--profile", "default", "--json"}, args...)...)
		return stderr, err
	}
	fields := func() map[string]string {
		t.Helper()
		db, err := storage.NewDatabase(home + "/.monoagent/monoagent.db")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		entries, err := secrets.List(context.Background(), db.DB, "default")
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Name == "aws" {
				f, _, err := secrets.DecryptFields(context.Background(), db.DB, "default", e.ID)
				if err != nil {
					t.Fatal(err)
				}
				return f
			}
		}
		t.Fatal("no aws entry")
		return nil
	}
	if _, _, err := runCLIIn(t, home, "", "--json", "db", "migrate"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("secret", "add", "--kind", "secret", "--name", "aws", "--field", "key_id=A1", "--field", "secret_key=S1", "--field", "region=eu"); err != nil {
		t.Fatal(err)
	}

	stderr, err := run("secret", "update", "aws", "--field", "secret_key=S2", "--merge")
	if err != nil || strings.Contains(stderr, "Warning") {
		t.Fatalf("a merge: %v\n%s", err, stderr)
	}
	if got := fields(); !reflect.DeepEqual(got, map[string]string{"key_id": "A1", "secret_key": "S2", "region": "eu"}) {
		t.Errorf("after a merge: %v", got)
	}

	stderr, err = run("secret", "update", "aws", "--field", "secret_key=S3")
	if err != nil || !strings.Contains(stderr, `this drops "key_id", "region"; pass --merge to keep them`) {
		t.Errorf("a replace that drops fields: %v\n%s", err, stderr)
	}
	if got := fields(); !reflect.DeepEqual(got, map[string]string{"secret_key": "S3"}) {
		t.Errorf("after a replace: %v", got)
	}

	if _, err := run("secret", "update", "aws", "--merge"); err == nil {
		t.Error("--merge without fields went through")
	}
}
