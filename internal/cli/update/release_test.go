package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/release"
)

// signedLatest serves a signed manifest over TLS and returns a client for it
// with generated keys; the returned legacy endpoint answers with v1.0.0.
func signedLatest(t *testing.T, tamper bool) (client func() *release.Client, legacyURL string) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	m, _ := json.Marshal(release.Manifest{Schema: 1, Version: "v2.0.0", ReleasedAt: "2026-10-08T10:00:00Z", NotesURL: "https://monoes.me/n"})
	sig := []byte(release.KeyID(pub) + " " + base64.StdEncoding.EncodeToString(ed25519.Sign(priv, m)))
	if tamper {
		m = bytes.Replace(m, []byte("v2.0.0"), []byte("v9.0.0"), 1)
	}
	mux := http.NewServeMux()
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, _ *http.Request) { w.Write(m) })
	mux.HandleFunc("/manifest.json.sig", func(w http.ResponseWriter, _ *http.Request) { w.Write(sig) })
	client = func() *release.Client {
		return &release.Client{HTTP: srv.Client(), Keys: []release.Key{{ID: release.KeyID(pub), Public: pub}},
			ManifestURLs: []string{srv.URL + "/manifest.json"}, Hosts: []string{"127.0.0.1"}, RedirectHosts: []string{"127.0.0.1"}}
	}
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"tag_name":"v1.0.0","html_url":"https://example.test/legacy"}`))
	}))
	t.Cleanup(legacy.Close)
	return client, legacy.URL
}

func TestFetchLatestPrefersSignedManifest(t *testing.T) {
	t.Parallel()
	client, legacyURL := signedLatest(t, false)
	info, err := FetchLatest(context.Background(), client(), legacyURL, false)
	if err != nil || info.Tag != "v2.0.0" || info.Manifest == nil {
		t.Fatalf("got %+v, %v", info, err)
	}
}

func TestFetchLatestTamperedManifestDoesNotFallBack(t *testing.T) {
	t.Parallel()
	client, legacyURL := signedLatest(t, true)
	info, err := FetchLatest(context.Background(), client(), legacyURL, false)
	if err == nil || info != nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("tampered manifest must fail, not fall back: %+v, %v", info, err)
	}
}

// Not parallel: it turns release.AllowLegacyGitHubUpdates off, which every
// fetch reads.
func TestFetchLatestNoPinnedKeyFallsBackToLegacy(t *testing.T) {
	_, legacyURL := signedLatest(t, false)
	info, err := FetchLatest(context.Background(), &release.Client{}, legacyURL, false)
	if err != nil || info.Tag != "v1.0.0" || info.Legacy == nil {
		t.Fatalf("got %+v, %v", info, err)
	}
	release.AllowLegacyGitHubUpdates = false
	t.Cleanup(func() { release.AllowLegacyGitHubUpdates = true })
	if _, err := FetchLatest(context.Background(), &release.Client{}, legacyURL, false); err == nil {
		t.Fatal("legacy disabled and no key must fail")
	}
}
