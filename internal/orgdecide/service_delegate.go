package orgdecide

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/orgbridge"
	"github.com/monoes/mono-agent/internal/orgdesign"
	"github.com/monoes/mono-agent/internal/orggrant"
)

// Delegator hooks, replaced in tests.
var (
	capabilityCheck = func(ctx context.Context) bool {
		set, err := monomind.Capabilities(ctx)
		return err == nil && set.Has(monomind.CapOrgToolProviders)
	}
	delegateSend = func(ctx context.Context, db *sql.DB, req orgbridge.SendRequest) error {
		_, err := orgbridge.Send(ctx, orgbridge.NewLedger(db), req)
		return err
	}
)

// delegate hands an item to a boss or parent decider role. It returns a
// reason (and no decision) when that decider cannot take the item, so the
// caller uses the fallback (C-50): the role raised the item itself, it has
// a gate of its own pending (every tool it has is blocked), it holds no
// decision tools, or monomind cannot give roles tools.
func (s *Service) delegate(ctx context.Context, profileID, root, org, level string, a *Autonomy, doc *orgdesign.Doc, it Item, record recordFunc) (*Decision, string, error) {
	if !capabilityCheck(ctx) {
		return nil, a.Decider.Kind + " decider needs monomind with capability org-tool-providers", nil
	}
	all, _, _ := orgdesign.LoadAll(root)
	dOrg, dRole, err := DeciderRole(a, doc, all)
	if err != nil {
		return nil, a.Decider.Kind + " decider unavailable: " + err.Error(), nil
	}
	if dOrg == org && it.Requester == dRole {
		return nil, "the decider role raised this item itself", nil
	}
	if raw, err := s.Client.Gates(ctx, root, dOrg); err == nil {
		if gates, err := ParseGates(raw); err == nil {
			for _, g := range gates {
				if g.Requester == dRole {
					return nil, "the decider role has its own gate pending", nil
				}
			}
		}
	}
	grants, _ := orggrant.NewStore(s.DB).ListGrants(ctx, profileID, dOrg, dRole)
	holds := false
	for _, g := range grants {
		for _, ot := range g.OrgTools {
			if ot.Tool == ToolDecisionResolve && containsString(ot.Orgs, org) {
				holds = true
			}
		}
	}
	if !holds {
		return nil, fmt.Sprintf("%s:%s holds no decision tools for %s (set them with `org autonomy set --decider %s`)", dOrg, dRole, org, a.Decider.Kind), nil
	}
	timeout := time.Duration(a.Decider.TimeoutSeconds) * time.Second
	del, err := s.Store.Delegate(ctx, profileID, org, level, dOrg, dRole, it, s.clock().Add(timeout))
	if err != nil {
		return nil, "", err
	}
	resolver := "boss:" + dRole
	if a.Decider.Kind == orgdesign.DeciderParent {
		resolver = "parent:" + dOrg + ":" + dRole
	}
	body := fmt.Sprintf("Decision %s for org %s: %s (class %s, tier %s).\nList pending decisions with monoagent__decision_list and resolve this one with monoagent__decision_resolve id=%q, verdict one of %s, and a one-sentence rationale. After %s without a decision it goes to %s.",
		del.ID, org, it.Summary, it.Class, it.Tier, del.ID, strings.Join(AllowedVerdicts(it.Kind, level), ", "), timeout, a.Decider.Fallback)
	if err := delegateSend(ctx, s.DB, orgbridge.SendRequest{
		ProfileID: profileID, Root: root, Org: dOrg, To: dRole, From: "operator:decisions",
		Subject: "[decision needed] " + it.Class + ": " + oneLine(it.Summary, 80), Body: body,
	}); err != nil {
		_, _ = s.Store.CloseDelegation(ctx, del.ID, DelegationExpired)
		return nil, "could not message the decider: " + err.Error(), nil
	}
	d, err := record(resolver, VerdictEscalated, "", "sent to "+resolver+"; fallback "+a.Decider.Fallback+" after "+timeout.String(), nil, nil)
	return d, "", err
}

// SweepDelegations sends every delegation whose decider timed out to the
// fallback model, and closes delegations whose item was resolved elsewhere.
func (s *Service) SweepDelegations(ctx context.Context, profileID, root string) {
	pending, err := s.Store.PendingDelegations(ctx, profileID)
	if err != nil {
		return
	}
	byOrg := map[string][]Delegation{}
	for _, d := range pending {
		byOrg[d.OrgName] = append(byOrg[d.OrgName], d)
	}
	for org, dels := range byOrg {
		a, err := s.Store.Get(ctx, profileID, org)
		if err != nil {
			continue
		}
		items, runID, partial, err := s.Pending(ctx, profileID, root, org, a)
		if err != nil {
			continue
		}
		// The pause brake applies here too: a paused org decides nothing, so
		// a delegation waits rather than falling back to the model. The level
		// is read now, not the one captured when the item was delegated.
		level := a.EffectiveLevel(s.clock())
		if level == orgdesign.LevelManual {
			continue
		}
		live := map[string]Item{}
		for _, it := range items {
			live[it.Kind+"|"+it.Ref] = it
		}
		doc, _ := orgdesign.Load(root, org)
		for _, d := range dels {
			it, stillPending := live[d.Item.Kind+"|"+d.Item.Ref]
			if !stillPending {
				// A partial list cannot tell "resolved elsewhere" from "the
				// source that holds it failed this tick"; closing here would
				// strand the item, because ProcessOrg skips anything whose
				// latest decision is an escalation at the current level.
				if partial {
					continue
				}
				_, _ = s.Store.CloseDelegation(ctx, d.ID, DelegationResolved)
				continue
			}
			if s.clock().Before(d.DeadlineAt) {
				continue
			}
			if ok, _ := s.Store.CloseDelegation(ctx, d.ID, DelegationExpired); !ok {
				continue
			}
			fallback := *a
			fallback.Decider.Kind = orgdesign.DeciderModel
			if _, err := s.decide(ctx, profileID, root, org, runID, level, &fallback, doc, it); err != nil {
				s.Logf("orgdecide: fallback for %s: %v", d.ID, err)
			}
		}
	}
}
