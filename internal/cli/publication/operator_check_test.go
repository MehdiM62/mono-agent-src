package publication

import (
	"strings"
	"testing"
)

// A Deps without its operator check refuses what only the operator may do
// to the history, rather than panicking (#864 review).
func TestPublicationHistoryWithoutAnOperatorCheckIsRefused(t *testing.T) {
	f := newFixture(t)
	for _, verb := range []string{"delete", "redact"} {
		d := f.deps(true)
		d.Operator = nil
		cmd := NewCommand(d)
		cmd.SetOut(&strings.Builder{})
		cmd.SetErr(&strings.Builder{})
		cmd.SetArgs([]string{verb, "pub_x"})
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), verb+" publication history: the operator check is not wired") {
			t.Errorf("%s without an operator check: %v", verb, err)
		}
	}
}
