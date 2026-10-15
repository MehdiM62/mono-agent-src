package clicore

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/automation"
	"github.com/monoes/mono-agent/internal/workflow"
)

func TestErrorsCarryTheirExitCode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		code int
		json string
	}{
		{NotFound("x %d", 1), 2, "not_found"},
		{InvalidInput("x"), 3, "invalid_input"},
		{AuthConnection("x"), 4, "auth_or_connection"},
		{fmt.Errorf("wrapped: %w", workflow.ErrWorkflowNotFound), 2, "not_found"},
		{workflow.ErrCycleDetected, 3, "invalid_input"},
		{errors.New("plain"), 1, ""},
	}
	for _, c := range cases {
		if got := ExitCodeFor(c.err); got != c.code {
			t.Errorf("ExitCodeFor(%v) = %d, want %d", c.err, got, c.code)
		}
		if got := JSONErrorCode(c.err); got != c.json {
			t.Errorf("JSONErrorCode(%v) = %q, want %q", c.err, got, c.json)
		}
	}
	if got := NotFound("person %q not found", "p1").Error(); got != `person "p1" not found` {
		t.Errorf("message = %q", got)
	}
}

type fieldsErr struct{}

func (fieldsErr) Error() string { return "boom" }
func (fieldsErr) JSONErrorFields() map[string]any {
	return map[string]any{"error": "ignored", "hint": "h"}
}

func TestWithJSONErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		json    bool
		err     error
		want    string
		reports bool
	}{
		{"plain", true, errors.New("bad"), `{"error":"bad"}`, true},
		{"classified", true, InvalidInput("no"), `{"code":"invalid_input","error":"no"}`, true},
		{"fields", true, fieldsErr{}, `{"error":"boom","hint":"h"}`, true},
		{"install result", true, WithInstallResult(errors.New("inst"), &automation.InstallResult{}),
			`{"error":"inst","issues":null,"result":`, true},
		{"already reported", true, Reported{Err: errors.New("r")}, ``, false},
		{"no --json", false, errors.New("bad"), ``, false},
	}
	for _, c := range cases {
		cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return c.err }}
		WithJSONErrors(func() bool { return c.json }, cmd)
		var out bytes.Buffer
		cmd.SetOut(&out)
		err := cmd.RunE(cmd, nil)
		if !bytes.HasPrefix(out.Bytes(), []byte(c.want)) || (c.want == "" && out.Len() != 0) {
			t.Errorf("%s: stdout %q, want prefix %q", c.name, out.String(), c.want)
		}
		var rep Reported
		if got := errors.As(err, &rep); got != (c.reports || errors.As(c.err, &rep)) {
			t.Errorf("%s: Reported = %v", c.name, got)
		}
		if err.Error() != c.err.Error() {
			t.Errorf("%s: err %q, want %q", c.name, err, c.err)
		}
	}
	// A command without RunE is left alone.
	cmd := &cobra.Command{Use: "group"}
	WithJSONErrors(func() bool { return true }, cmd)
	if cmd.RunE != nil {
		t.Error("RunE added to a group")
	}
}

func TestWithInstallResultKeepsNil(t *testing.T) {
	t.Parallel()
	if WithInstallResult(nil, &automation.InstallResult{}) != nil {
		t.Error("nil error wrapped")
	}
	plain := errors.New("x")
	if WithInstallResult(plain, nil) != plain {
		t.Error("error without result wrapped")
	}
}

func TestOutputHelpers(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	if err := WriteJSONTo(&b, map[string]int{"a": 1}); err != nil || b.String() != "{\n  \"a\": 1\n}\n" {
		t.Errorf("WriteJSONTo = %q, %v", b.String(), err)
	}
	for _, c := range []struct {
		s    string
		n    int
		want string
	}{{"hello", 10, "hello"}, {"hello world", 8, "hello..."}, {"hello", 2, "he"}} {
		if got := TruncateStr(c.s, c.n); got != c.want {
			t.Errorf("TruncateStr(%q, %d) = %q", c.s, c.n, got)
		}
	}
	b.Reset()
	table := NewPlainTable(&b, []string{"Field", "Value"}, []tw.Align{tw.AlignRight, tw.AlignLeft})
	table.Append([]string{"ID", "a-very-long-value-that-is-not-wrapped-at-all-by-the-table"})
	table.Render()
	// No outer border, no wrapping, the first column right-aligned.
	want := " FIELD │                           VALUE                           \n" +
		"───────┼───────────────────────────────────────────────────────────\n" +
		"    ID │ a-very-long-value-that-is-not-wrapped-at-all-by-the-table \n"
	if b.String() != want {
		t.Errorf("table:\n%s", b.String())
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := ExpandPath("~/x/y.db"); got != filepath.Join(home, "x/y.db") {
		t.Errorf("ExpandPath = %q", got)
	}
	if got := ExpandPath("/abs/y.db"); got != "/abs/y.db" {
		t.Errorf("ExpandPath = %q", got)
	}
}

func TestOpenAutomationRegistryUnderTheTestHome(t *testing.T) {
	reg, err := OpenAutomationRegistry("")
	if err != nil || reg == nil {
		t.Fatalf("OpenAutomationRegistry: %v", err)
	}
}
