package dynorg

import (
	"context"
	"slices"
	"sort"
	"strings"
)

// maxConcerns bounds a suggested team: past it the lead splits the work
// further itself.
const maxConcerns = 4

// trivialWords: a brief this short that names at most one concern is a
// one-step job.
const trivialWords = 14

// Concerns lists the distinct kinds of work a brief names (one built-in
// role each, the best-matching first). Two concerns in one brief are what
// can run in parallel or need different specialties.
func Concerns(brief string) []builtinRole {
	type hit struct {
		r builtinRole
		n int
	}
	var hits []hit
	for _, r := range slices.Concat(appRoles, coreRoles) { // the specific app roles win ties
		if n := r.hits(brief); n > 0 {
			hits = append(hits, hit{r, n})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].n > hits[j].n })
	var out []builtinRole
	for _, h := range hits {
		if len(out) == maxConcerns {
			break
		}
		out = append(out, h.r)
	}
	return out
}

// SuggestTeam is the smallest team for a brief: nothing when it is a short
// one-step job the lead should do itself, else one worker per distinct
// concern (one when it names just one). Each worker is staffed with
// its own role (never two with the same role) and the cheapest model that
// fits its category.
func (s *Staffer) SuggestTeam(ctx context.Context, brief string) ([]Staff, error) {
	concerns := Concerns(brief)
	if len(concerns) <= 1 && len(strings.Fields(brief)) <= trivialWords {
		return nil, nil
	}
	if len(concerns) == 0 {
		concerns = []builtinRole{builtinFor(brief)}
	}
	team := make([]Staff, 0, len(concerns))
	for _, c := range concerns {
		st, err := s.Staff(ctx, SpawnRequest{Brief: brief, Role: c.ID})
		if err != nil {
			return nil, err
		}
		team = append(team, st)
	}
	return team, nil
}
