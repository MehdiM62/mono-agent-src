package jevcli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// Without its operator check, what only the user does refuses rather than
// panics (#845 code review).
func TestJevCommandsWithoutAnOperatorCheckRefuse(t *testing.T) {
	cfg := jevTestCfg(t, true)
	for _, args := range [][]string{{"enable", "api_auto", "--yes"}, {"key", "set"}, {"key", "adopt"}, {"key", "remove"}} {
		d := cfg.deps()
		d.Refusal = nil
		cmd := NewCommand(d)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("a-key\n"))
		cmd.SetArgs(args)
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		err := cmd.Execute()
		var fields interface{ JSONErrorFields() map[string]any }
		if !errors.As(err, &fields) || fields.JSONErrorFields()["code"] != "operator_only" || !strings.Contains(err.Error(), "operator check is not wired") {
			t.Errorf("%v without an operator check: %v", args, err)
		}
	}
}
