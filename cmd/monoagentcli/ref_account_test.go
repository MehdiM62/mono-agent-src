package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRefAccountDocumentsTheLiveBuild(t *testing.T) {
	cmd := refAccountCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"The account gate is live", "2026-10-11", "production signing key is pinned", "24 hours", "login_required", "keyring_unavailable", "unconfirmed", "MONOAGENT_FILE_KEYRING_PASSPHRASE_FILE", "Never copy or restore", "license (LICENSE, section 4) forbids bypassing the check"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("ref account omits %q", want)
		}
	}
	assertNoDormantClaims(t, "ref account", out.String())
}

// The examples must remain usable if command flags or aliases change.
func TestRefAccountFlagsExistOnCommands(t *testing.T) {
	root := newRootCmd()
	for _, tc := range []struct {
		path  []string
		flags []string
	}{
		{[]string{"account", "login"}, []string{"email", "send", "code", "code-stdin", "no-browser", "timeout"}},
		{[]string{"account", "status"}, []string{"offline"}},
		{[]string{"library", "login"}, []string{"email", "send", "code", "code-stdin", "no-browser", "timeout"}},
		{[]string{"library", "status"}, []string{"offline"}},
	} {
		cmd, _, err := root.Find(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		for _, flag := range tc.flags {
			if cmd.Flags().Lookup(flag) == nil {
				t.Errorf("%v no longer has --%s", tc.path, flag)
			}
		}
	}
	cmd, _, err := root.Find([]string{"ref", "account"})
	if err != nil || cmd.Name() != "account" {
		t.Fatalf("ref account missing: %v", err)
	}
	applyClassification(root)
	if commandClass(cmd) != classOpen {
		t.Fatal("offline account reference requires sign-in")
	}
}

// The page `ref account` and the `ref commands` entries say what the gate does,
// in release R's terms: the gate is live.
func TestRefAccountDocumentsTheGate(t *testing.T) {
	out := captureStdout(t, func() { c := refAccountCmd(); c.Run(c, nil) })
	for _, w := range append(spelled(""), refusalLine(), "login_required", "exit 4", graceText(), "account login --email", "--send", "--code",
		"MONOAGENT_ALLOW_FILE_KEYRING", "MONOAGENT_FILE_KEYRING_PASSPHRASE_FILE", "-tags devaccount", "No credential comes from an environment variable",
		"Never copy or restore", "signs out the original and every copy", "MONOAGENT_DEV_ENFORCE_FROM", "monoes..account", "core.monoes_account",
		"enforce_from", "2026-10-11", "Exit 4") {
		if !strings.Contains(flat(out), flat(w)) {
			t.Errorf("`ref account` does not say %q", w)
		}
	}
	assertListText(t, "`ref account`", out, "Open:", openCommands)
	assertListText(t, "`ref account`", out, "Serving:", servingCommands)
}

func TestRefListsTheAccountTopicAndCommands(t *testing.T) {
	ref := newRefCmd()
	found := false
	for _, c := range ref.Commands() {
		found = found || c.Name() == "account"
	}
	listing := captureStdout(t, func() { _ = ref.RunE(ref, nil) })
	if !found || !strings.Contains(listing, "account") || !strings.Contains(ref.Long, "account") {
		t.Error("`ref` must have an `account` topic, listed by `ref` and named in its help")
	}
	seen := map[string]int{}
	for _, d := range cliDocs {
		seen[d.Name]++
	}
	for _, name := range []string{"account login", "account status", "account logout"} {
		if seen[name] != 1 {
			t.Errorf("`ref commands` must document %q exactly once, has %d entries", name, seen[name])
		}
	}
}

// Every flag `ref commands` documents for the account commands exists on the real command.
func TestRefCommandsAccountFlagsExist(t *testing.T) {
	root := newRootCmd()
	for path, flags := range map[string][]string{"account login": {"email", "send", "code", "code-stdin", "no-browser", "timeout"}, "account status": {"offline"}} {
		c, _, err := root.Find(strings.Fields(path))
		if err != nil || c == root {
			t.Fatalf("no command %q", path)
		}
		for _, d := range cliDocs {
			for _, f := range flags {
				if d.Name == path && !strings.Contains(d.Flags+d.Usage, "--"+f) {
					t.Errorf("`ref commands` does not document --%s of %q", f, path)
				}
			}
		}
		for _, f := range flags {
			if c.Flag(f) == nil {
				t.Errorf("`ref commands` documents --%s for %q, which has no such flag", f, path)
			}
		}
	}
}
