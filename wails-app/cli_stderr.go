package main

import "strings"

// cliWarningPrefixes start the advisory lines the CLI prints on stderr ahead
// of any real error: the account-gate notices (cmd/monoagentcli
// account_gate_refusal.go announce) and zerolog's JSON warnings. The GUI shows
// the account state on its own, so these lines only bury the actual error —
// and they tell a GUI user to run a terminal command.
var cliWarningPrefixes = []string{
	"A monoes.me login will be required from ",
	"monoes.me is unreachable; this login works offline until ",
	"This login can no longer be renewed on this machine",
	`{"level":"warn"`,
	`{"level":"info"`,
	`{"level":"debug"`,
}

// cliStderr is the CLI's stderr with the advisory lines dropped and the rest
// trimmed — the text a GUI error message should carry.
func cliStderr(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	kept := lines[:0]
	for _, l := range lines {
		if isCLIWarningLine(l) {
			continue
		}
		kept = append(kept, l)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func isCLIWarningLine(line string) bool {
	t := strings.TrimSpace(line)
	for _, p := range cliWarningPrefixes {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}
