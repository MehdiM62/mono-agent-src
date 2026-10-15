package browserjev

import (
	"context"
	"fmt"

	"github.com/monoes/mono-agent/internal/browserctl"
)

// runGate is what a run an AI caller started is held to (gate.go): the
// submit guard on its tab, and for an agent's shell the origin policy too.
// A run the user started gets one that holds nothing.
type runGate struct {
	who   caller
	ai    bool
	pol   *browserctl.Policy // an agent shell's
	guard submitGuard
	end   *gateEnd // how the gate ended the run, if it did
}

// newRunGate is the gate of a run in ctx that starts at startURL.
func newRunGate(ctx context.Context, startURL string) (*runGate, error) {
	g := &runGate{}
	g.who, g.ai = aiCaller(ctx)
	if g.who.shell {
		pol, err := shellPolicy(ctx)
		if err != nil {
			return nil, err
		}
		g.pol = &pol
		if err := g.allowed(startURL); err != nil {
			return nil, err
		}
	}
	return g, nil
}

// allowed checks a page the run is on against an agent shell's origin
// policy: `monoagentcli browser`'s, with the user's allowlist.
func (g *runGate) allowed(pageURL string) error {
	if g.pol == nil || pageURL == "" || pageURL == "about:blank" {
		return nil
	}
	if err := g.pol.Check(pageURL); err != nil {
		return fmt.Errorf("%s started this run: %w", g.who.name, err)
	}
	return nil
}

// arm arms the submit guard on the run's tab; a run that can't is refused.
func (g *runGate) arm(ctx context.Context, drv driver) error {
	if !g.ai {
		return nil
	}
	gd, ok := drv.(guardable)
	if !ok {
		return fmt.Errorf("%s started this run, and this browser can't hold its form submits for the user's approval", g.who.name)
	}
	var err error
	if g.guard, err = gd.ArmSubmitGuard(ctx); err != nil {
		return fmt.Errorf("%s started this run, and the guard that holds its form submits for approval failed (update the extension?): %w", g.who.name, err)
	}
	return nil
}

// beforeAct guards the page about to be acted on, which may have moved to
// another origin since the guard was armed.
func (g *runGate) beforeAct(ctx context.Context, pageURL string) error {
	if g.guard == nil {
		return nil
	}
	return g.guard.Follow(ctx, pageURL)
}

// afterAct settles what the guard blocked during an action of kind; a
// non-nil end finishes the run with its status and reason.
func (g *runGate) afterAct(ctx context.Context, kind string) (*gateEnd, error) {
	if g.guard == nil {
		return nil, nil
	}
	if kind == "click" || kind == "fill" || kind == "select" {
		waitForGuard(ctx) // a submit the action set off reaches the guard's record
	}
	end, err := settleSubmits(ctx, g.guard, g.who)
	g.end = end
	return end, err
}

// keepTab: the tab of a submit still waiting stays open for `browser submit
// --hil`.
func (g *runGate) keepTab() bool { return g.end != nil && g.end.status == "awaiting_approval" }

// output adds the item, and the tab of a waiting submit, to the result.
func (g *runGate) output(out map[string]interface{}) {
	if g.end == nil {
		return
	}
	out["hil_id"] = g.end.hilID
	if g.keepTab() {
		out["tab_id"] = g.guard.TabID()
	}
}
