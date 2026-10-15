package daemon

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func profileCmd(args ...string) (*cobra.Command, *bytes.Buffer) {
	c := &cobra.Command{Use: "daemon"}
	c.PersistentFlags().String("profile", "", "")
	var errBuf bytes.Buffer
	c.SetErr(&errBuf)
	_ = c.ParseFlags(args)
	return c, &errBuf
}

func TestDaemonProfileNeedsAProfile(t *testing.T) {
	c, _ := profileCmd()
	if _, err := daemonProfile(c, Deps{ProfileID: func() string { return "" }}); err == nil {
		t.Fatal("a daemon with no profile started")
	}
}

func TestDaemonProfileWarnsWithoutTheFlag(t *testing.T) {
	c, errBuf := profileCmd()
	got, err := daemonProfile(c, Deps{ProfileID: func() string { return "p-1" }})
	if err != nil || got != "p-1" || !strings.Contains(errBuf.String(), "--profile") {
		t.Fatalf("got %q, %v, stderr %q", got, err, errBuf.String())
	}
	c, errBuf = profileCmd("--profile", "p-1")
	if _, err := daemonProfile(c, Deps{ProfileID: func() string { return "p-1" }}); err != nil || errBuf.Len() != 0 {
		t.Fatalf("explicit profile: %v, stderr %q", err, errBuf.String())
	}
}
