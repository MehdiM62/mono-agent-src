package automation

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func bootTree() *cobra.Command {
	root := &cobra.Command{Use: "monoagentcli"}
	run := func(*cobra.Command, []string) {}
	auto := &cobra.Command{Use: "automation"}
	auto.AddCommand(&cobra.Command{Use: "list", Run: run})
	comp := &cobra.Command{Use: "completion"}
	comp.AddCommand(&cobra.Command{Use: "bash", Run: run})
	root.AddCommand(auto, comp, &cobra.Command{Use: "version", Run: run}, &cobra.Command{Use: "help", Run: run},
		&cobra.Command{Use: cobra.ShellCompRequestCmd, Run: run}, &cobra.Command{Use: "daemon", Run: run})
	return root
}

// Help, version and completion skip the boot; everything else, a bare group included, boots, and
// a failed boot is a warning.
func TestBoot(t *testing.T) {
	t.Parallel()
	for args, want := range map[string]bool{
		"automation list": true, "automation": true, "daemon": true, "": true,
		"version": false, "help": false, "completion bash": false, cobra.ShellCompRequestCmd: false,
	} {
		cmd, _, err := bootTree().Find(strings.Fields(args))
		if err != nil {
			t.Fatal(err)
		}
		if NeedsAutomations(cmd) != want {
			t.Errorf("NeedsAutomations(%q) = %v", args, !want)
		}
		booted := false
		var stderr bytes.Buffer
		Boot(cmd, func() error { booted = true; return nil }, &stderr)
		if booted != want || stderr.Len() != 0 {
			t.Errorf("%q: booted=%v stderr=%q", args, booted, stderr.String())
		}
	}
	cmd, _, _ := bootTree().Find([]string{"daemon"})
	var stderr bytes.Buffer
	Boot(cmd, func() error { return errors.New("bad fold") }, &stderr)
	if stderr.String() != "warning: automations: bad fold (using ~/.monoagent/actions only)\n" {
		t.Errorf("stderr = %q", stderr.String())
	}
}
