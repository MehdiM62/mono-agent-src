package dynorg

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// Routers: who chooses a worker's role and skills when the lead names none
// (setting coder.router). monomind's registered agents and skills come
// first in every mode; the built-in roles are the fallback.
const (
	RouterJev   = "jev"   // Jev chooses from pick's shortlist
	RouterModel = "model" // the lead names the role from org_roster
	RouterAuto  = "auto"  // pick when confident, else Jev, else the lead (default)
)

// NormalizeRouter maps anything unknown to RouterAuto.
func NormalizeRouter(r string) string {
	switch r {
	case RouterJev, RouterModel:
		return r
	}
	return RouterAuto
}

// maxSkills is the most skills a worker gets: every skill is prompt text
// the worker pays for.
const maxSkills = 3

const noSkill = "none"

// routeRole staffs the role when the lead named none. It reports false
// when no monomind agent was chosen and the caller should use the
// built-in fallback.
func (s *Staffer) routeRole(ctx context.Context, req SpawnRequest, st *Staff) (bool, error) {
	router := NormalizeRouter(s.Router)
	if router == RouterModel {
		return false, fmt.Errorf("choose a role for this worker (role); org_roster lists the registry agents by category and the built-in roles")
	}
	app, hasApp := appRoleFor(req.Brief)
	if s.Picker == nil || s.Library == nil {
		return false, nil
	}
	list, confident, err := s.Picker.Agents(ctx, req.Brief)
	if err != nil || len(list) == 0 {
		return false, nil
	}
	if router == RouterAuto && confident {
		return s.useAgent(ctx, list[0], "role from monomind pick", st), nil
	}
	if router == RouterAuto && hasApp {
		st.setBuiltin(app, "app-aware built-in role (monomind pick was not sure)")
		return true, nil
	}
	opts := map[string]string{}
	for _, c := range list {
		opts[c.ID] = strings.TrimSpace(c.Name + ": " + c.Description)
	}
	if hasApp {
		opts[app.ID] = app.Title + ": " + firstSentence(app.Body)
	}
	if s.Chooser != nil && len(opts) > 1 {
		if id, conf, err := s.Chooser.Choose(ctx, map[string]any{"brief": req.Brief}, "Which agent should do this brief?", opts); err == nil {
			if i := slices.IndexFunc(list, func(c Candidate) bool { return c.ID == id }); i >= 0 {
				if s.useAgent(ctx, list[i], "role from Jev over monomind pick's shortlist", st) {
					st.JevConf = &conf
					return true, nil
				}
			}
			if hasApp && id == app.ID {
				st.JevConf = &conf
				st.setBuiltin(app, "app-aware built-in role chosen by Jev")
				return true, nil
			}
		}
	}
	if router == RouterJev && confident {
		return s.useAgent(ctx, list[0], "role from monomind pick (Jev gave no answer)", st), nil
	}
	if hasApp {
		st.setBuiltin(app, "app-aware built-in role (no pick was sure)")
		return true, nil
	}
	short := make([]string, 0, len(list))
	for _, c := range list {
		short = append(short, c.ID)
	}
	return false, fmt.Errorf("monomind is not sure which agent fits and Jev could not decide; choose a role (role): %s", strings.Join(short, ", "))
}

// useAgent loads a registry agent into st.
func (s *Staffer) useAgent(ctx context.Context, c Candidate, why string, st *Staff) bool {
	body, title, cat, err := s.Library.AgentBody(ctx, c.ID)
	if err != nil {
		return false
	}
	p := c.Probability
	st.Role, st.AgentType, st.Category, st.AgentBody = orElse(title, orElse(c.Name, c.ID)), c.ID, orElse(cat, c.Category), body
	st.PickConf = &p
	st.Why = append(st.Why, why)
	return true
}

// routeSkills names the skills for a brief when the lead gave none: at most
// maxSkills, and none when nothing is clearly needed.
func (s *Staffer) routeSkills(ctx context.Context, brief string) []string {
	router := NormalizeRouter(s.Router)
	if s.Picker == nil || router == RouterModel {
		return nil
	}
	list, confident, err := s.Picker.Skills(ctx, brief)
	if err != nil || len(list) == 0 {
		return nil
	}
	if confident && router == RouterAuto {
		return topSkills(list)
	}
	if s.Chooser == nil {
		if confident {
			return topSkills(list)
		}
		return nil
	}
	opts := map[string]string{noSkill: "no skill is needed; the role is enough"}
	for _, c := range list {
		opts[c.ID] = strings.TrimSpace(c.Name + ": " + c.Description)
	}
	id, _, err := s.Chooser.Choose(ctx, map[string]any{"brief": brief}, "Which skill, if any, does this brief need? Fewer is cheaper.", opts)
	if err != nil || id == noSkill || !slices.ContainsFunc(list, func(c Candidate) bool { return c.ID == id }) {
		return nil
	}
	return []string{id}
}

// topSkills keeps pick's best skills (at most maxSkills), dropping those
// ranked far below the first.
func topSkills(list []Candidate) []string {
	var out []string
	for _, c := range list {
		if len(out) == maxSkills || len(out) > 0 && list[0].Probability > 0 && c.Probability < list[0].Probability/2 {
			break
		}
		out = append(out, c.ID)
	}
	return out
}

func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i > 0 {
		return s[:i+1]
	}
	return s
}
