package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// pflag reads the first `backticked` word of a flag's usage as its value
// placeholder, so "see `agent scan`" prints as `--no-scan agent scan`. A
// placeholder never holds a space; a backticked phrase is an accident.
func TestFlagUsagesHaveNoAccidentalPlaceholders(t *testing.T) {
	// Owned by other files' owners; drop each entry once its usage is fixed.
	pending := map[string]bool{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			name, _ := pflag.UnquoteUsage(f)
			key := c.CommandPath() + " --" + f.Name
			if strings.Contains(name, " ") && !pending[key] {
				t.Errorf("%s: usage turns %q into the value placeholder: %q", key, name, f.Usage)
			}
		})
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newRootCmd())
}
