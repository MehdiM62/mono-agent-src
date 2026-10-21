package extensioncli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/storage"
)

func seedPaired(t *testing.T, home string, list ...extension.PairedBrowser) {
	t.Helper()
	dir := filepath.Join(home, ".monoagent")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(list)
	if err := os.WriteFile(filepath.Join(dir, "extension.browsers.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBrowsersListAndRevoke(t *testing.T) {
	pairTestHome(t)
	home, _ := os.UserHomeDir()
	seedPaired(t, home,
		extension.PairedBrowser{ID: "aaaa1111bbbb2222", Label: "Edge Work", Origin: "chrome-extension://x", Agent: "Edg/140", PairedAt: time.Now()},
		extension.PairedBrowser{ID: "cccc3333dddd4444", Label: "Chrome Home", Origin: "chrome-extension://x", PairedAt: time.Now()})
	d := testDeps(false)

	out, err := runExt(t, d, "browsers", "list")
	if err != nil || !strings.Contains(out, "Edge Work") || !strings.Contains(out, "Chrome Home") {
		t.Fatalf("list = %q, %v", out, err)
	}
	if strings.Contains(out, "tokenHash") {
		t.Fatalf("list shows token material: %q", out)
	}
	if _, err := runExt(t, d, "browsers", "revoke", "aaa"); err == nil {
		t.Fatal("a 3-character prefix should be refused")
	}
	out, err = runExt(t, d, "browsers", "revoke", "aaaa")
	if err != nil || !strings.Contains(out, "Revoked aaaa1111bbbb2222") {
		t.Fatalf("revoke = %q, %v", out, err)
	}
	if left, _ := extension.ListPairedBrowsers(); len(left) != 1 || left[0].Label != "Chrome Home" {
		t.Fatalf("left = %+v", left)
	}
	if _, err := runExt(t, d, "browsers", "revoke", "aaaa"); err == nil {
		t.Fatal("revoking a revoked browser should fail")
	}
}

// A marked process (an agent's shell, a workflow run) cannot revoke.
func TestBrowsersRevokeIsOperatorOnly(t *testing.T) {
	pairTestHome(t)
	home, _ := os.UserHomeDir()
	seedPaired(t, home, extension.PairedBrowser{ID: "aaaa1111bbbb2222", Label: "Edge Work", PairedAt: time.Now()})
	t.Setenv("CLAUDECODE", "1")
	d := testDeps(false)
	d.Refusal = func() string { return "an agent shell" }
	_, err := runExt(t, d, "browsers", "revoke", "aaaa")
	if !operatorOnlyErr(err) {
		t.Fatalf("revoke from a marked process = %v, want operator_only", err)
	}
	if left, _ := extension.ListPairedBrowsers(); len(left) != 1 {
		t.Fatalf("a refused revoke removed the browser: %+v", left)
	}
}

func TestExtensionPairModeReadsSetting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(home, "monoagent.db")
	if got := extensionPairMode(path)(); got != extension.PairModeAsk {
		t.Fatalf("no database = %q, want ask", got)
	}
	db, err := storage.NewDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	if got := extensionPairMode(path)(); got != extension.PairModeAsk {
		t.Fatalf("no entry = %q, want ask", got)
	}
	for v, want := range map[string]string{"false": "off", "FALSE": "off", "0": "off", "off": "off", " no ": "off", "auto": "auto", "ask": "ask", "on": "ask", "true": "ask", "junk": "ask"} {
		_ = db.SetSetting(autoPairSetting, v)
		if got := extensionPairMode(path)(); got != want {
			t.Errorf("extension.autoPair=%q = %q, want %q", v, got, want)
		}
	}
}

func TestAutoPairCommandSetsTheMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(home, "monoagent.db")
	d := testDepsDB(false, path)
	out, err := runExt(t, d, "autopair")
	if err != nil || !strings.Contains(out, "asks you") {
		t.Fatalf("autopair = %q, %v", out, err)
	}
	if out, err = runExt(t, d, "autopair", "auto"); err != nil || !strings.Contains(out, "automatic") {
		t.Fatalf("autopair auto = %q, %v", out, err)
	}
	if got := extensionPairMode(path)(); got != "auto" {
		t.Fatalf("the bridge's reader sees %q", got)
	}
	if out, err = runExt(t, d, "autopair", "off"); err != nil || !strings.Contains(out, "is off") {
		t.Fatalf("autopair off = %q, %v", out, err)
	}
	for _, bad := range []string{"maybe", "on"} {
		if _, err := runExt(t, d, "autopair", bad); err == nil {
			t.Fatalf("%q should be refused", bad)
		}
	}
	d.Refusal = func() string { return "an agent shell" }
	if _, err := runExt(t, d, "autopair", "auto"); !operatorOnlyErr(err) {
		t.Fatalf("an agent switching to auto = %v, want operator_only", err)
	}
	if got := extensionPairMode(path)(); got != "off" {
		t.Fatalf("a refused change moved the mode to %q", got)
	}
}

func TestExtensionApproveAndDeny(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	d := testDepsDB(false, filepath.Join(home, "monoagent.db"))
	a, _, _ := extension.CreatePendingClaim(extension.PendingClaim{Label: "Edge", Origin: "chrome-extension://aaaa"})
	b, _, _ := extension.CreatePendingClaim(extension.PendingClaim{Label: "Chrome", Origin: "chrome-extension://bbbb"})

	if _, err := runExt(t, d, "approve"); err == nil {
		t.Fatal("approve with neither an id nor --latest should fail")
	}
	// An agent's shell cannot approve; the claim stays waiting.
	d.Refusal = func() string { return "an agent shell" }
	if _, err := runExt(t, d, "approve", "--latest"); !operatorOnlyErr(err) {
		t.Fatalf("approve from a marked process = %v, want operator_only", err)
	}
	if _, err := runExt(t, d, "deny", a.ID); !operatorOnlyErr(err) {
		t.Fatalf("deny from a marked process = %v, want operator_only", err)
	}
	if left, _ := extension.ListPendingClaims(); len(left) != 2 {
		t.Fatalf("a refused decision changed the queue: %+v", left)
	}
	d.Refusal = func() string { return "" }
	out, err := runExt(t, d, "approve", "--latest")
	if err != nil || !strings.Contains(out, b.ID) || !strings.Contains(out, "Chrome") {
		t.Fatalf("approve --latest = %q, %v", out, err)
	}
	if out, err = runExt(t, d, "deny", a.ID[:5]); err != nil || !strings.Contains(out, "Denied "+a.ID) {
		t.Fatalf("deny = %q, %v", out, err)
	}
	_, err = runExt(t, d, "approve", "--latest")
	if err == nil {
		t.Fatal("nothing is waiting any more")
	}
}
