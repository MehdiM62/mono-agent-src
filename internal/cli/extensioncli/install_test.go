package extensioncli

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/updatecheck"
)

// fixtureExtensionZip builds a zip shaped like the release asset: every file
// under one chrome-extension/ folder.
func fixtureExtensionZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeExtensionRelease serves zip and a SHA256SUMS.txt naming sumsDigest for
// it, under both the tagged and the latest download paths.
func fakeExtensionRelease(t *testing.T, zipData []byte, sumsDigest string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/"+extensionZipAsset):
			_, _ = w.Write(zipData)
		case strings.HasSuffix(r.URL.Path, "/"+updatecheck.SumsAssetName):
			_, _ = w.Write([]byte(sumsDigest + "  " + extensionZipAsset + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := extensionReleasesURL
	extensionReleasesURL = srv.URL + "/releases"
	t.Cleanup(func() { extensionReleasesURL = old })
}

func runExtensionInstall(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newExtensionInstallCmd(testDeps(false))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	// A nil slice would make cobra read os.Args.
	cmd.SetArgs(append([]string{}, args...))
	err := cmd.Execute()
	return out.String(), err
}

func TestExtensionDownloadBase(t *testing.T) {
	old := extensionReleasesURL
	extensionReleasesURL = "https://example.test/releases"
	defer func() { extensionReleasesURL = old }()
	cases := map[string]string{
		"v0.134.1":            "https://example.test/releases/download/v0.134.1",
		"dev":                 "https://example.test/releases/latest/download",
		"v0.134.1-3-gabc1234": "https://example.test/releases/latest/download",
		"":                    "https://example.test/releases/latest/download",
	}
	for v, want := range cases {
		if got := extensionDownloadBase(v); got != want {
			t.Errorf("extensionDownloadBase(%q) = %q, want %q", v, got, want)
		}
	}
}

func TestExtensionInstallDownloadsVerifiesAndUnpacks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	data := fixtureExtensionZip(t, map[string]string{
		"chrome-extension/manifest.json":    `{"manifest_version":3}`,
		"chrome-extension/icons/icon16.png": "png",
	})
	fakeExtensionRelease(t, data, updatecheck.SHA256Hex(data))

	out, err := runExtensionInstall(t)
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	dir := filepath.Join(home, ".monoagent", "extension")
	for _, f := range []string{"manifest.json", "icons/icon16.png"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s not unpacked: %v", f, err)
		}
	}
	for _, want := range []string{"Checksum verified", "chrome://extensions", "edge://extensions", "Developer mode",
		"Load unpacked and choose " + dir, "run `monoagentcli extension install` again and click Reload"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestExtensionInstallRefusesAChecksumMismatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	data := fixtureExtensionZip(t, map[string]string{"chrome-extension/manifest.json": "{}"})
	fakeExtensionRelease(t, data, strings.Repeat("0", 64))

	if _, err := runExtensionInstall(t); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("want a checksum mismatch, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".monoagent", "extension")); !os.IsNotExist(err) {
		t.Errorf("something was installed after a failed check: %v", err)
	}
}

func TestExtensionInstallReplacesThePreviousCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".monoagent", "extension")
	first := fixtureExtensionZip(t, map[string]string{
		"chrome-extension/manifest.json": "v1",
		"chrome-extension/removed.js":    "old",
	})
	if err := installExtensionZip(first, dir); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(t.TempDir(), "ext.zip")
	if err := os.WriteFile(second, fixtureExtensionZip(t, map[string]string{"chrome-extension/manifest.json": "v2"}), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runExtensionInstall(t, "--zip", second)
	if err != nil {
		t.Fatalf("install --zip: %v\n%s", err, out)
	}
	if !strings.Contains(out, "not checked against a release") {
		t.Errorf("a local zip must say it was not checked:\n%s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "manifest.json")); string(b) != "v2" {
		t.Errorf("manifest.json = %q, want v2", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "removed.js")); !os.IsNotExist(err) {
		t.Errorf("a file the new zip lacks survived the reinstall")
	}
	entries, _ := os.ReadDir(filepath.Dir(dir))
	if len(entries) != 1 {
		t.Errorf("leftover staging folders next to the extension: %v", entries)
	}
}

func TestExtensionInstallKeepsThePreviousCopyOnABadZip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "extension")
	if err := installExtensionZip(fixtureExtensionZip(t, map[string]string{"chrome-extension/manifest.json": "v1"}), dir); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"zip slip":    fixtureExtensionZip(t, map[string]string{"../evil.js": "x", "manifest.json": "{}"}),
		"no manifest": fixtureExtensionZip(t, map[string]string{"chrome-extension/background.js": "x"}),
		"not a zip":   []byte("not a zip"),
	}
	for name, data := range cases {
		if err := installExtensionZip(data, dir); err == nil {
			t.Errorf("%s: installed", name)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, "manifest.json")); string(b) != "v1" {
			t.Errorf("%s: the previous extension was not kept (manifest.json = %q)", name, b)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.js")); !os.IsNotExist(err) {
		t.Errorf("zip slip wrote outside the extension folder")
	}
}

func TestExtensionInstallIsRegistered(t *testing.T) {
	for _, c := range newExtensionCmd(testDeps(false)).Commands() {
		if c.Name() == "install" {
			return
		}
	}
	t.Fatal("`extension install` is not registered")
}
