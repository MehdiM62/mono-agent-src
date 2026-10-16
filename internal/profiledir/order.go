package profiledir

import "time"

// The one definition of "oldest profile", shared by the extension bridge's
// default marker (List) and the desktop app's first selection. The database
// stores created_at in two shapes ("2006-01-02 15:04:05" for the seeded
// default row, RFC3339 for rows the CLI creates), so a plain string compare
// would order them by the character between date and time, not by time.

var createdLayouts = []string{
	"2006-01-02 15:04:05",
	time.RFC3339Nano,
	"2006-01-02T15:04:05",
}

// ParseCreated reads a profiles.created_at in either stored format.
func ParseCreated(s string) (time.Time, bool) {
	for _, l := range createdLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// Older reports whether the profile (createdA, idA) comes before (createdB,
// idB): older created_at first, ties (the same second) broken by id. A
// created_at that cannot be read sorts after every readable one, then by its
// text, so the order is still total and the same on every start.
func Older(createdA, idA, createdB, idB string) bool {
	ta, okA := ParseCreated(createdA)
	tb, okB := ParseCreated(createdB)
	switch {
	case okA && okB:
		if !ta.Equal(tb) {
			return ta.Before(tb)
		}
	case okA != okB:
		return okA
	case createdA != createdB:
		return createdA < createdB
	}
	return idA < idB
}
