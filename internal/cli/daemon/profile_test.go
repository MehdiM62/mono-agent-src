package daemon

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// The daemon serves every profile and runs each workflow under its own, so it
// takes no profile. A service unit written by an earlier release still carries
// --profile <id>: the flag is accepted (hidden, no-op) so that unit keeps
// starting, and the daemon says once that it is ignored.

func daemonUnderRoot(t *testing.T) (*cobra.Command, *cobra.Command) {
	t.Helper()
	root := &cobra.Command{Use: "monoagentcli"}
	root.PersistentFlags().String("profile", "", "")
	d := NewCommand(Deps{})
	root.AddCommand(d)
	return root, d
}

func TestDaemonAcceptsTheDeprecatedProfileFlag(t *testing.T) {
	_, d := daemonUnderRoot(t)
	f := d.Flags().Lookup("profile")
	if f == nil {
		t.Fatal("daemon has no --profile: a unit that carries it would no longer start")
	}
	if !f.Hidden {
		t.Error("the deprecated --profile is advertised in the daemon's help")
	}
	if err := d.ParseFlags([]string{"--profile", "p-1"}); err != nil {
		t.Fatalf("daemon --profile p-1: %v", err)
	}
	if !d.Flags().Changed("profile") {
		t.Error("--profile was parsed but not seen as set")
	}
}

// The no-op flag does not reach the root's --profile, which would select a
// profile for the database and engine the daemon opens.
func TestDaemonProfileFlagSelectsNothing(t *testing.T) {
	root, _ := daemonUnderRoot(t)
	root.SetArgs([]string{"daemon", "--profile", "p-1", "--help"})
	root.SetOut(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := root.PersistentFlags().Lookup("profile").Value.String(); got != "" {
		t.Errorf("the root --profile became %q: the daemon would act on it", got)
	}
}

func TestDeprecatedProfileIsAnnouncedOnceAndOnlyWhenGiven(t *testing.T) {
	_, d := daemonUnderRoot(t)
	var errBuf bytes.Buffer
	d.SetErr(&errBuf)

	noteDeprecatedProfile(d)
	if errBuf.Len() != 0 {
		t.Fatalf("no --profile, yet stderr says %q", errBuf.String())
	}
	if err := d.ParseFlags([]string{"--profile", "p-1"}); err != nil {
		t.Fatal(err)
	}
	noteDeprecatedProfile(d)
	got := errBuf.String()
	if strings.Count(got, "\n") != 1 || !strings.Contains(got, "--profile") || !strings.Contains(got, "deprecated") {
		t.Fatalf("want one deprecation line about --profile, got %q", got)
	}
}
