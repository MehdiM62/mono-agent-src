package workflowgenerate

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/ai/generate"
	"github.com/monoes/mono-agent/internal/cli/clicore"
)

// Saved without --json: the draft's name and id, its counts and the runtime, and the next step.
func TestGenerateTextSaved(t *testing.T) {
	t.Parallel()
	_, cfg := persistTestEnv(t)
	cfg.JSONOutput = false
	turns := new(int)
	var system string
	exec := func(_ context.Context, runtime, model string) (generate.ExecFunc, error) {
		if runtime != "fake" || model != "m1" {
			t.Errorf("runtime/model = %q/%q", runtime, model)
		}
		return func(_ context.Context, _, sys string) (string, error) {
			system = sys
			*turns++
			return genValid, nil
		}, nil
	}
	out, err := runGenerate(t, testDeps(cfg, exec, nil), "fetch", "--runtime", "fake", "--model", "m1", "--name", "Feed")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Saved draft workflow: Feed  (id: ", "  nodes: 2   validation: valid   attempts: 1   runtime: fake / m1\n",
		"  It is inactive. Review it with `monoagentcli workflow get "} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// The catalog is titled from the `ref node` docs.
	if !strings.Contains(system, "Make an HTTP request") {
		t.Error("the catalog does not use FindNodeDoc")
	}
}

// The refusals before any agent turn, and an agent that cannot start: exit codes and, with
// --json, one {"error","code"} document from clicore.WithJSONErrors.
func TestGenerateRefusals(t *testing.T) {
	t.Parallel()
	_, cfg := persistTestEnv(t)
	noAgent := func(context.Context, string, string) (generate.ExecFunc, error) {
		return nil, errors.New("agent exploded")
	}
	cases := []struct {
		args []string
		code int
		doc  string
	}{
		{[]string{"  "}, 3, `{"code":"invalid_input","error":"a description is required"}`},
		{[]string{"x", "--max-attempts", "0"}, 3, `{"code":"invalid_input","error":"--max-attempts must be at least 1"}`},
		{[]string{"x", "--runtime", "fake"}, 1, `{"error":"agent exploded"}`},
	}
	for _, c := range cases {
		out, err := runGenerate(t, testDeps(cfg, noAgent, nil), c.args...)
		if clicore.ExitCodeFor(err) != c.code || strings.TrimSpace(out) != c.doc {
			t.Errorf("%v: exit %d, out %q; want %d, %s", c.args, clicore.ExitCodeFor(err), out, c.code, c.doc)
		}
		var rep clicore.Reported
		if !errors.As(err, &rep) {
			t.Errorf("%v: the JSON document is not marked as printed", c.args)
		}
	}
}

// --save=false is --no-save; with --json the record carries the document and saved=false.
func TestGenerateSaveFalseJSON(t *testing.T) {
	t.Parallel()
	_, cfg := persistTestEnv(t)
	turns := new(int)
	out, err := runGenerate(t, testDeps(cfg, fakeExec(t, turns, genValid), nil), "fetch", "--runtime", "fake", "--save=false")
	if err != nil {
		t.Fatal(err)
	}
	var got generateOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got.Saved || got.ID != "" || got.NextAction != "" || got.Runtime != "fake" || len(got.Workflow.Nodes) != 2 {
		t.Fatalf("record = %+v", got)
	}
}
