package accountcli

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestServingCommandStartsTheGuardBeforeItsOwnPreRun(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		when []func(*cobra.Command) bool
		want string
	}{
		{"always", nil, "start prerun"},
		{"when it serves", []func(*cobra.Command) bool{func(*cobra.Command) bool { return true }}, "start prerun"},
		{"when it does not serve", []func(*cobra.Command) bool{func(*cobra.Command) bool { return false }}, "prerun"},
	} {
		var got []string
		cmd := &cobra.Command{Use: "serve", Run: func(*cobra.Command, []string) {},
			PreRun: func(*cobra.Command, []string) { got = append(got, "prerun") }}
		ServingCommand(cmd, func(context.Context) { got = append(got, "start") }, c.when...)
		cmd.SetArgs(nil)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if strings.Join(got, " ") != c.want {
			t.Errorf("%s: %v, want %s", c.name, got, c.want)
		}
	}
	// No PreRun of its own: the guard still starts.
	started := 0
	cmd := ServingCommand(&cobra.Command{Use: "serve", Run: func(*cobra.Command, []string) {}}, func(context.Context) { started++ })
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil || started != 1 {
		t.Errorf("started %d times: %v", started, err)
	}
}
