package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/release"
	"github.com/zalando/go-keyring"
)

func TestReleaseKeygenAndVerify(t *testing.T) {
	keyring.MockInit()
	var out bytes.Buffer
	cmd := newReleaseCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"keygen"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	i := strings.Index(out.String(), "\t\"")
	if i < 0 {
		t.Fatalf("no key line in %q", out.String())
	}
	line, err := strconv.Unquote(strings.TrimSuffix(strings.TrimSpace(out.String()[i:]), ","))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "PRIVATE") {
		t.Fatal("private key printed")
	}
	// Shared contract: service monoagent-release-signing, account ed25519-v1,
	// value = base64 of the 64-byte private key, whose public half is the printed key.
	stored, err := keyring.Get("monoagent-release-signing", "ed25519-v1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(stored)
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		t.Fatalf("stored key is %d bytes (%v), want %d", len(raw), err, ed25519.PrivateKeySize)
	}
	if got := release.KeyLine(ed25519.PrivateKey(raw).Public().(ed25519.PublicKey)); got != line {
		t.Fatalf("printed %q, stored key is %q", line, got)
	}
	// A second keygen refuses to replace the key.
	cmd = newReleaseCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"keygen"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("keygen replaced an existing key")
	}

	// verify with an explicit key: sign with a fresh pair (keygen's private key stays in the keychain).
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	dir := t.TempDir()
	mf := filepath.Join(dir, "manifest.json")
	sf := mf + ".sig"
	m := []byte(`{"schema":1,"version":"v3.0.0","released_at":"2026-10-08T10:00:00Z","assets":[]}`)
	os.WriteFile(mf, m, 0o600)
	os.WriteFile(sf, []byte(release.KeyID(pub)+" "+base64.StdEncoding.EncodeToString(ed25519.Sign(priv, m))), 0o600)
	run := func(key string) (string, error) {
		var o bytes.Buffer
		c := newReleaseCmd()
		c.SetOut(&o)
		c.SetArgs([]string{"verify", mf, sf, "--pubkey", key})
		err := c.Execute()
		return o.String(), err
	}
	if o, err := run(release.KeyLine(pub)); err != nil || !strings.Contains(o, "v3.0.0") {
		t.Fatalf("verify: %q %v", o, err)
	}
	if _, err := run(line); err == nil {
		t.Fatal("verified against the wrong key")
	}
	os.WriteFile(mf, append(m, ' '), 0o600)
	if _, err := run(release.KeyLine(pub)); err == nil {
		t.Fatal("verified a tampered manifest")
	}
}
