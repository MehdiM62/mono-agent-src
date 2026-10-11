package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/account"
)

// docText reads a file of the repository whose claims these tests keep true.
func docText(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// flat makes a text searchable: lower case, one line, single spaces.
func flat(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// staleClaims stopped being true once a monoes.me sign-in became part of the
// product (spec D26). A bare "offline" is not one: the `ref` manual is offline,
// two commands have an --offline flag, and the grace message says "works
// offline until".
var staleClaims = []string{"local-first", "no telemetry", "phone-home", "phone home", "fully offline", "runs entirely on your machine",
	"nothing is sent to us", "no outbound calls on its own behalf", "does not depend on any hosted backend"}

func staleHits(text string) (hits []string) {
	for _, c := range staleClaims {
		if strings.Contains(flat(text), c) {
			hits = append(hits, c)
		}
	}
	return hits
}

func assertNoStaleClaims(t *testing.T, rel string) {
	t.Helper()
	for _, c := range staleHits(docText(t, rel)) {
		t.Errorf("%s still says %q: rewrite it to what is true (spec D26)", rel, c)
	}
}

func assertSays(t *testing.T, rel string, wants ...string) {
	t.Helper()
	text := flat(docText(t, rel))
	for _, w := range wants {
		if !strings.Contains(text, flat(w)) {
			t.Errorf("%s does not say %q", rel, w)
		}
	}
}

// What the documents quote from the contract, so that nothing is retyped.
func graceText() string { return fmt.Sprintf("%d hours", int(account.GraceWindow.Hours())) }

func refusalLine() string {
	err := &account.LoginRequiredError{Status: account.Status{State: account.StateLocked, Reason: account.ReasonNotLoggedIn}}
	return strings.SplitN(err.Error(), "\n", 2)[0]
}

// spelled is every state and reason, each wrapped in wrap (a backtick, or nothing).
func spelled(wrap string) (out []string) {
	for _, s := range []account.State{account.StateOK, account.StateGrace, account.StateLocked} {
		out = append(out, wrap+string(s)+wrap)
	}
	for _, r := range []account.Reason{account.ReasonNotLoggedIn, account.ReasonExpired, account.ReasonRefused, account.ReasonClockRollback,
		account.ReasonClockSkew, account.ReasonKeyUnknown, account.ReasonInvalid, account.ReasonUnreachable, account.ReasonServerError,
		account.ReasonKeyringUnavailable, account.ReasonUnconfirmed} {
		out = append(out, wrap+string(r)+wrap)
	}
	return out
}

func TestStaleClaimsLeaveTheOfflineManualAndFlagsAlone(t *testing.T) {
	for _, ok := range []string{"the offline manual", "`library status [--offline]`", "all offline, always current", "this login works offline until <time>"} {
		if hits := staleHits(ok); len(hits) != 0 {
			t.Errorf("%q is true and must not be flagged: %v", ok, hits)
		}
	}
	for _, bad := range []string{"Local-first n8n alternative", "No telemetry: no phone-home checks", "it works fully offline", "nothing is sent to us"} {
		if len(staleHits(bad)) == 0 {
			t.Errorf("%q is stale and must be flagged", bad)
		}
	}
}

// The front-door documents say what is true of release R without claiming that
// enforcement is live: the account "will be required" from the enforcement date
// (account status shows it as enforce_from), and TestAccountDocsMatchDormantRollout
// keeps the "dormant" statements until a date is set.
func TestFrontDoorDocsStateTheAccount(t *testing.T) {
	assertNoStaleClaims(t, "README.md")
	assertNoStaleClaims(t, "SUPPORT.md")
	assertSays(t, "README.md", "A monoes.me account will be required", "monoagentcli account login", "account login --email", "account logout",
		"login_required", graceText(), "SECURITY.md#network-use-telemetry-and-crash-reporting", "AGENTS.md#headless-and-docker",
		"MONOAGENT_ALLOW_FILE_KEYRING", "MONOAGENT_FILE_KEYRING_PASSPHRASE_FILE", "SECURITY.md#one-sign-in-per-machine-never-copied",
		"enforce_from", "dormant")
	assertSays(t, "SUPPORT.md", "account status --json", "doctor --check core.monoes_account")
	assertSays(t, "install.sh", "account login")
}
