package dynorg

import (
	"context"
	"sort"
	"strings"
)

// AgentInfo is one registered monomind agent as org_roster lists it.
type AgentInfo struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
}

// AgentLister is implemented by a Library that can list its registered
// agents by category (MonomindLibrary reads .monomind/registry.json).
type AgentLister interface {
	ListAgents(ctx context.Context) map[string][]AgentInfo
}

const (
	maxRosterAgents  = 120 // agents listed in org_roster, so it stays cheap to read
	maxRosterDescLen = 90
)

// SuggestedWorker is one worker of the team org_roster suggests for a brief.
type SuggestedWorker struct {
	Role   string `json:"role"`
	Access string `json:"access"`
	Model  string `json:"model"`
}

// registryAgents lists the registry's agents by category, one line each,
// capped so a big registry doesn't flood the lead's context.
func (c *Conductor) registryAgents(ctx context.Context) map[string][]AgentInfo {
	l, ok := c.cfg.Staffer.Library.(AgentLister)
	if !ok {
		return nil
	}
	all := l.ListAgents(ctx)
	cats := make([]string, 0, len(all))
	for k := range all {
		cats = append(cats, k)
	}
	sort.Strings(cats)
	out := map[string][]AgentInfo{}
	n := 0
	for _, cat := range cats {
		list := all[cat]
		sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
		for _, a := range list {
			if n == maxRosterAgents {
				return out
			}
			a.Description = clipLine(a.Description, maxRosterDescLen)
			out[cat] = append(out[cat], a)
			n++
		}
	}
	return out
}

// suggestTeam is the smallest team for a brief (Staffer.SuggestTeam), for
// org_roster's optional brief argument. A brief nothing needs a team for
// returns an empty list: the lead should do it itself.
func (c *Conductor) suggestTeam(ctx context.Context, brief string) []SuggestedWorker {
	team, err := c.cfg.Staffer.SuggestTeam(ctx, brief)
	out := []SuggestedWorker{}
	if err != nil {
		return out
	}
	for _, st := range team {
		out = append(out, SuggestedWorker{Role: st.AgentType, Access: st.Access, Model: st.Model.Key()})
	}
	return out
}

func clipLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
