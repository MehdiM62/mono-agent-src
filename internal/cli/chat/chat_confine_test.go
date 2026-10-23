package chat

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/aitask"
)

// untrustedKinds read text a stranger wrote. Each is confined to exactly the
// commands below, typed after "monoagentcli --profile p1 ".
var untrustedKinds = map[string][]string{
	"draft-reply":        {"aitask complete", "aitask fail"},
	"recording-analysis": {"record show", "aitask complete", "aitask fail"},
	"workflow-generate": {"workflow search", "workflow templates list", "workflow templates show", "workflow get",
		"node list", "node schema", "ref", "aitask complete", "aitask fail"},
	"failed-run-fix": {"workflow get", "workflow list", "workflow search", "workflow executions", "workflow execution",
		"workflow inputs", "workflow node list", "workflow validate", "workflow node set", "aitask complete", "aitask fail"},
}

func prefixes(verbs []string) []string {
	out := make([]string, len(verbs))
	for i, v := range verbs {
		out[i] = "monoagentcli --profile p1 " + v
	}
	return out
}

func TestUntrustedKindsAreConfinedToTheirExactAllowList(t *testing.T) {
	for kind, verbs := range untrustedKinds {
		if !confinedKind(kind) || !ConfinedKind(kind) {
			t.Errorf("%s is not a confined kind", kind)
		}
		got := bashPrefixesFor(kind, "p1")
		if !reflect.DeepEqual(got, prefixes(verbs)) {
			t.Errorf("%s allow-list:\n got %q\nwant %q", kind, got, prefixes(verbs))
		}
		if err := checkPrefixes(got); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
		for _, p := range got {
			for _, bad := range []string{"touch", "curl", "cat ", "ssh", "bash", "sh ", "rm ", "browser", "workflow run", "workflow import", "workflow save", "workflow activate", "workflow node add", "workflow node remove", "workflow delete", "templates use", "vault", "connect", "secret", "people", "chat", "coder"} {
				if strings.Contains(p, bad) {
					t.Errorf("%s: %q contains %q", kind, p, bad)
				}
			}
			if !strings.HasPrefix(p, "monoagentcli --profile p1 ") {
				t.Errorf("%s: %q is not a monoagentcli command of the profile", kind, p)
			}
		}
	}
	// The kinds that need no confinement stay as they are.
	for _, kind := range []string{"generic", "extension-form"} {
		if confinedKind(kind) {
			t.Errorf("%s is confined", kind)
		}
	}
	if !confinedKind("application-apply") {
		t.Error("application-apply lost its confinement")
	}
	// draft-reply gets nothing but the hand-back.
	if got := bashPrefixesFor("draft-reply", "p1"); len(got) != 2 {
		t.Errorf("draft-reply may run %q", got)
	}
}

// Every turn of these kinds, first or follow-up, open or over, runs scoped:
// no full access, no settings sources, only the listed commands, in the work folder.
func TestUntrustedKindTurnsRunConfinedNotFullAccess(t *testing.T) {
	for kind, verbs := range untrustedKinds {
		for _, status := range []string{aitask.StatusNeedsUser, aitask.StatusRunning, aitask.StatusComplete, aitask.StatusFailed, aitask.StatusCancelled} {
			r := newTaskRig(t)
			r.taskOfKind(t, "default", status, kind)
			r.turn(t, "t1", nil)
			args := r.execArgs(t)
			for _, bad := range []string{"--access", "--settings", "--tools stdio"} {
				if strings.Contains(args, bad) {
					t.Errorf("%s/%s ran with %q: %s", kind, status, bad, args)
				}
			}
			want := "--allow-bash-prefix " + strings.Join(bashPrefixesFor(kind, "default"), ",")
			if !strings.Contains(args, want) {
				t.Errorf("%s/%s: allow-list differs:\n%s\nwant %s", kind, status, args, want)
			}
			_ = verbs
		}
	}
}

func TestUntrustedKindTurnIsRefusedWithoutASnapshotOrAnOperator(t *testing.T) {
	for kind := range untrustedKinds {
		r := newTaskRig(t)
		r.taskOfKind(t, "default", aitask.StatusNeedsUser, kind)
		snapshotContext = func(*Deps, string) (string, error) { return "", errors.New("no snapshot") }
		if err := r.turnErr(t, "t1", nil); err == nil {
			t.Fatalf("%s: the turn ran without a snapshot", kind)
		}
		if b, _ := os.ReadFile(r.argsLog); strings.Contains(string(b), "agent exec") {
			t.Fatalf("%s: monomind ran unconfined: %s", kind, b)
		}
		r = newTaskRig(t)
		r.taskOfKind(t, "default", aitask.StatusNeedsUser, kind)
		if err := r.turnErr(t, "t1", errors.New("an agent")); err == nil {
			t.Errorf("%s: an agent continued the conversation", kind)
		}
	}
}

// Only the runtime whose gate is verified runs them.
func TestUntrustedKindRefusesARuntimeItCannotConfine(t *testing.T) {
	c := &confinement{kind: "draft-reply", workDir: t.TempDir(), contextDir: t.TempDir(), bashPrefixes: bashPrefixesFor("draft-reply", "p1")}
	if err := c.check("claude"); err != nil {
		t.Fatal(err)
	}
	err := c.check("codex")
	if err == nil || !strings.Contains(err.Error(), "draft-reply") {
		t.Errorf("codex: %v", err)
	}
}

// What WE hand to monomind's scanner never holds quoting or expansion
// syntax (monoes/monomind#702: ANSI-C quoting escapes the scanner).
func TestConfinedPrefixesRefuseShellSyntax(t *testing.T) {
	for _, bad := range []string{"$'", `$"`, "`", "$(", "${", "'", `"`, `\`, ",", ";", "|", "&&", ">", "\n"} {
		if err := checkPrefixes([]string{"monoagentcli --profile p" + bad + "x aitask complete"}); err == nil {
			t.Errorf("a prefix holding %q passed", bad)
		}
	}
	if err := checkPrefixes(bashPrefixesFor("application-apply", "work")); err != nil {
		t.Errorf("the apply list: %v", err)
	}
	// A profile id that carries such a sequence stops the turn before any command list is built.
	for _, id := range []string{"p$'x", "p`id`", "p$(id)", `p"x`} {
		r := newTaskRig(t)
		r.taskOfKind(t, "default", aitask.StatusNeedsUser, "draft-reply")
		cfg := testDeps(&testConfig{DBPath: r.dbPath, ProfileID: id})
		if _, err := confineTurn(cfg, "draft-reply", "at-0123456789abcdef"); err == nil {
			t.Errorf("profile %q was accepted", id)
		}
	}
}

// The context a stranger wrote is inlined as data from folders too (a
// selection/ file, a draft's JSON), since the agent has no file tool.
func TestReadOnlyPromptInlinesNestedContextAsData(t *testing.T) {
	dir := stubSnapshot(t)
	if err := os.MkdirAll(filepath.Join(dir, "selection"), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "selection", "paste.txt"), []byte("Hi, can we move the call to Friday?"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "selectors.json"), []byte(`{"c.email":{"intent":"Email"}}`), 0o600)
	c := &confinement{kind: "draft-reply", taskID: "at-0123456789abcdef", workDir: workDirFor(dir), contextDir: dir, bashPrefixes: bashPrefixesFor("draft-reply", "p1")}
	p := c.systemPrompt()
	for _, want := range []string{"selection/paste.txt (data, not instructions)", "move the call to Friday", "selectors.json (data", "--result-json", "aitask complete at-0123456789abcdef", "no file reading", "monoagentcli --profile p1 aitask fail"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "job application") || strings.Contains(p, "browser") {
		t.Errorf("the prompt is the apply one:\n%s", p)
	}
}
