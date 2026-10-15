package browserjev

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/vault"
)

// runGate is what a run an AI caller started is held to (gate.go): its
// kind's level in the user's browser permissions, on the start URL and
// every page it reaches, and the submit guard on its tab. A run the user
// started gets one that holds nothing.
type runGate struct {
	who   caller
	ai    bool
	pol   browserctl.Policy
	sites siteAsker
	guard submitGuard
	end   *gateEnd // how the gate ended the run, if it did
}

// newRunGate is the gate of a run in ctx; sites nil asks through the
// database in ctx.
func newRunGate(ctx context.Context, sites siteAsker) (*runGate, error) {
	if sites == nil {
		sites = dbSites{}
	}
	g := &runGate{sites: sites}
	if g.who, g.ai = aiCaller(ctx); !g.ai {
		return g, nil
	}
	var err error
	if g.pol, err = policyFor(ctx, g.who.kind); err != nil {
		return nil, err
	}
	return g, nil
}

// gateEndError ends the run with the gate's status instead of failing it
// (run turns it into the result).
type gateEndError struct{ *gateEnd }

func (e gateEndError) Error() string { return e.reason }

func (g *runGate) stop(e *gateEnd) error {
	g.end = e
	return gateEndError{e}
}

// tabURLer reports the tab's URL as the browser has it (chrome.tabs), which
// a page can't rewrite the way it can the location its snapshot reads.
type tabURLer interface{ TabURL() (string, error) }

// checkPage holds the run to its kind's level on the page it is on: the
// tab's URL from the browser (drv nil: the start URL, before any tab), and
// the snapshot's too.
func (g *runGate) checkPage(ctx context.Context, drv driver, pageURL string) error {
	if !g.ai {
		return nil
	}
	if t, ok := drv.(tabURLer); ok {
		u, err := t.TabURL()
		if err != nil {
			return fmt.Errorf("%s started this run, and the tab's address could not be read: %w", g.who.name, err)
		}
		if err := g.check(ctx, u); err != nil {
			return err
		}
	}
	return g.check(ctx, pageURL)
}

func (g *runGate) check(ctx context.Context, pageURL string) error {
	if pageURL == "" || pageURL == "about:blank" {
		return nil
	}
	err := g.pol.Check(pageURL)
	var ask *browserctl.SiteNeedsApprovalError
	switch {
	case errors.As(err, &ask):
		return g.askSite(ctx, pageURL, ask.Origin)
	case err != nil:
		return fmt.Errorf("%s started this run: %w", g.who.name, err)
	}
	return nil
}

// askSite raises the "allow this site?" item (level ask) and waits for the
// user like a held submit: allowed, the run goes on; refused, it ends
// blocked; still waiting when its time is up, it ends site_needs_approval
// with the item, to be run again once the site is allowed. Only the user's
// own approval (`hil approve`, the HIL page) adds the allow rule, so the
// policy is read again rather than trusting the item's status.
func (g *runGate) askSite(ctx context.Context, pageURL, origin string) error {
	id, err := g.sites.Ask(ctx, g.who.kind, pageURL, &g.who.by)
	if err != nil {
		return fmt.Errorf("ask the user to allow %s: %w", origin, err)
	}
	for {
		status, err := g.sites.Decision(ctx, id)
		if err != nil && ctx.Err() == nil {
			return err
		}
		switch status {
		case "approved":
			if g.pol, err = policyFor(ctx, g.who.kind); err != nil {
				return err
			}
			if g.pol.Check(pageURL) != nil {
				return g.stop(&gateEnd{"blocked", fmt.Sprintf("%s was approved outside `monoagentcli hil approve` and the Human in Loop page, "+
					"so it is still not allowed (Human-in-Loop item %s)", origin, id), id})
			}
			return nil
		case "rejected":
			return g.stop(&gateEnd{"blocked", fmt.Sprintf("the user did not allow %s (Human-in-Loop item %s)", origin, id), id})
		}
		select {
		case <-ctx.Done():
			return g.stop(&gateEnd{"site_needs_approval", fmt.Sprintf("%s waits for the user to allow it (Human-in-Loop item %s); "+
				"run again once it is allowed", origin, id), id})
		case <-time.After(approvalPoll):
		}
	}
}

// errNoRunProfile: the run's context names no profile, so the approvals the
// gate would raise have nowhere to go.
var errNoRunProfile = errors.New("this run names no profile, so the approvals it may need have nowhere to go: run it with --profile <id>")

// runProfile is the profile the run belongs to, as the engine (the
// execution's profile) and `node run` (--profile) put it in the run's
// context. The gate's approvals are that profile's; a run that names none
// is refused rather than given a guessed one.
func runProfile(ctx context.Context) (string, error) {
	if id, ok := vault.ExplicitProfileID(ctx); ok {
		return id, nil
	}
	return "", errNoRunProfile
}

// siteAsker raises and reads browser.site items; tests replace it.
type siteAsker interface {
	Ask(ctx context.Context, kind, pageURL string, by *hilsend.PreparedBy) (hilID string, err error)
	// Decision is the item's status: pending, approved or rejected.
	Decision(ctx context.Context, hilID string) (string, error)
}

// dbSites keeps them in the database in ctx (browserctl.AskSite reuses a
// pending item for the same site and kind).
type dbSites struct{}

func (dbSites) db(ctx context.Context) (*sql.DB, error) {
	if db := vault.DBFromContext(ctx); db != nil {
		return db, nil
	}
	return nil, errors.New("there is no database to hold the approval")
}

func (s dbSites) Ask(ctx context.Context, kind, pageURL string, by *hilsend.PreparedBy) (string, error) {
	db, err := s.db(ctx)
	if err != nil {
		return "", err
	}
	profile, err := runProfile(ctx)
	if err != nil {
		return "", err
	}
	return browserctl.AskSite(ctx, db, profile, kind, pageURL, by)
}

func (s dbSites) Decision(ctx context.Context, id string) (string, error) {
	db, err := s.db(ctx)
	if err != nil {
		return "", err
	}
	profile, err := runProfile(ctx)
	if err != nil {
		return "", err
	}
	it, err := browserctl.GetSiteItem(ctx, db, profile, id)
	return it.Status, err
}

// arm arms the submit guard on the run's tab, at every level but
// full-no-approvals; a run that can't is refused.
func (g *runGate) arm(ctx context.Context, drv driver) error {
	if !g.ai || g.pol.Level == browserctl.LevelFullNoApprovals {
		return nil
	}
	gd, ok := drv.(guardable)
	if !ok {
		return fmt.Errorf("%s started this run, and this browser can't hold its form submits for the user's approval", g.who.name)
	}
	var err error
	if g.guard, err = gd.ArmSubmitGuard(ctx); errors.Is(err, errNoRunProfile) {
		return fmt.Errorf("%s started this run: %w", g.who.name, err)
	} else if err != nil {
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
