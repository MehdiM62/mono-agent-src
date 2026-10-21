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

func TestExtensionAutoPairReadsSetting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(home, "monoagent.db")
	if !extensionAutoPair(path)() {
		t.Fatal("no database must leave auto-pairing on")
	}
	db, err := storage.NewDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	if !extensionAutoPair(path)() {
		t.Fatal("no entry must leave auto-pairing on")
	}
	for _, off := range []string{"false", "FALSE", "0", "off", " no "} {
		if err := db.SetSetting(autoPairSetting, off); err != nil {
			t.Fatal(err)
		}
		if extensionAutoPair(path)() {
			t.Errorf("extension.autoPair=%q left auto-pairing on", off)
		}
	}
	for _, on := range []string{"true", "on"} {
		_ = db.SetSetting(autoPairSetting, on)
		if !extensionAutoPair(path)() {
			t.Errorf("extension.autoPair=%q turned it off", on)
		}
	}
}

func TestAutoPairCommandSetsTheSwitch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(home, "monoagent.db")
	d := testDepsDB(false, path)
	out, err := runExt(t, d, "autopair")
	if err != nil || !strings.Contains(out, "is on") {
		t.Fatalf("autopair = %q, %v", out, err)
	}
	if out, err = runExt(t, d, "autopair", "off"); err != nil || !strings.Contains(out, "is off") {
		t.Fatalf("autopair off = %q, %v", out, err)
	}
	if extensionAutoPair(path)() {
		t.Fatal("the bridge's reader still sees it on")
	}
	if _, err := runExt(t, d, "autopair", "maybe"); err == nil {
		t.Fatal("a value other than on/off should be refused")
	}
	d.Refusal = func() string { return "an agent shell" }
	if _, err := runExt(t, d, "autopair", "on"); !operatorOnlyErr(err) {
		t.Fatalf("an agent turning it on = %v, want operator_only", err)
	}
}
