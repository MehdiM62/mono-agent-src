package nodes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/monoes/mono-agent/internal/action"
	"github.com/monoes/mono-agent/internal/workflow"
)

// A browser action of a package that isn't built-in (recorded, local,
// imported, or one whose trust can't be read) runs only for the user: an
// agent (a marker), a step the user hasn't approved, or a workflow such a
// one called is refused before any tab, whatever the action declares. A
// built-in package's action runs for them as before (its sends wait for
// approval in hilsend), and the user runs any.
func TestAnAgentRunsNoBrowserActionOfAPackageThatIsntBuiltIn(t *testing.T) {
	prev := globalSessionProvider
	t.Cleanup(func() { action.SetDefSource(nil); SetGlobalSessionProvider(prev) })
	agents := map[string]func(t *testing.T) context.Context{
		"an agent's shell": func(t *testing.T) context.Context {
			t.Setenv("CLAUDECODE", "1")
			return context.Background()
		},
		"another agent's shell": func(t *testing.T) context.Context {
			t.Setenv("AI_AGENT", "1")
			return context.Background()
		},
		"an unapproved step": func(*testing.T) context.Context {
			return workflow.WithAIAuthored(context.Background())
		},
		"a workflow an unapproved one called": func(*testing.T) context.Context {
			return workflow.WithTrigger(context.Background(), workflow.TriggerTypeExecuteWorkflow,
				map[string]interface{}{workflow.UnapprovedCallerKey: "wf-agent"})
		},
	}
	for _, trust := range []string{"recorded", "local", "imported", ""} {
		for who, ctxOf := range agents {
			t.Run(trust+"/"+who, func(t *testing.T) {
				action.SetDefSource(liveSource{pkg: livePkg{trust: trust, confirmed: true}, effects: liveEffects})
				p := &countingProvider{}
				SetGlobalSessionProvider(p)
				_, err := NewBrowserNode("acme", "list").Execute(ctxOf(t), workflow.NodeInput{}, map[string]interface{}{})
				if !errors.Is(err, ErrUserOnlyBrowserRun) || p.calls != 0 {
					t.Fatalf("err = %v, tabs %d; want ErrUserOnlyBrowserRun before any tab", err, p.calls)
				}
			})
		}
	}
	for who, ctxOf := range map[string]func(t *testing.T) context.Context{
		"an agent, on a built-in package": agents["an agent's shell"],
		"the user, on a recorded package": func(*testing.T) context.Context { return context.Background() },
	} {
		t.Run(who, func(t *testing.T) {
			trust := "recorded"
			if who == "an agent, on a built-in package" {
				trust = "builtin"
			}
			action.SetDefSource(liveSource{pkg: livePkg{trust: trust}, effects: liveEffects})
			p := &countingProvider{}
			SetGlobalSessionProvider(p)
			_, err := NewBrowserNode("acme", "list").Execute(ctxOf(t), workflow.NodeInput{}, map[string]interface{}{})
			if errors.Is(err, ErrUserOnlyBrowserRun) || p.calls != 1 {
				t.Fatalf("err = %v, tabs %d; want it to reach the browser", err, p.calls)
			}
		})
	}
}

// A package an agent writes by hand, a folder under ~/.monoagent/actions,
// becomes a local package (local-<p>, also under its own name), whose
// actions an agent doesn't run either.
func TestAnAgentRunsNoHandWrittenLocalPackage(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "actions", "handmade")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "press.json"), []byte(`{"actionType":"press","steps":[{"id":"go","type":"click","selector":"#go"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BootAutomations(home); err != nil {
		t.Fatal(err)
	}
	prev := globalSessionProvider
	t.Cleanup(func() { action.SetDefSource(nil); SetGlobalSessionProvider(prev) })
	r := workflow.NewNodeTypeRegistry()
	RegisterBrowserNodes(r)
	t.Setenv("CLAUDECODE", "1")
	for _, typ := range []string{"local-handmade.press", "handmade.press"} {
		f, ok := r.Get(typ)
		if !ok {
			t.Fatalf("%s is not registered (%v)", typ, r.Types())
		}
		p := &countingProvider{}
		SetGlobalSessionProvider(p)
		if _, err := f().Execute(context.Background(), workflow.NodeInput{}, map[string]interface{}{}); !errors.Is(err, ErrUserOnlyBrowserRun) || p.calls != 0 {
			t.Errorf("%s: err = %v, tabs %d; want ErrUserOnlyBrowserRun before any tab", typ, err, p.calls)
		}
	}
}
