package peoplereview

import (
	"errors"
	"testing"
)

// Only the user approves a person for outreach, and an approval covers the
// text it approved (#831 S5).
func TestNotOperatorCategory(t *testing.T) {
	approved := Stored{Found: true, Category: Approved, Introduction: "Hi Ada"}
	queued := Stored{Found: true, Category: Pending, Introduction: "Hi Ada"}
	for _, tc := range []struct {
		name     string
		stored   Stored
		category string
		set      bool
		intro    string
		want     string
		refused  bool
	}{
		{"a new person approved", Stored{}, Approved, true, "Hi", "", true},
		{"approved in other spelling", queued, " APPROVED ", true, "Hi Ada", "", true},
		{"a queued person approved", queued, Approved, true, "Hi Ada", "", true},
		{"an approved person, new text, still approved", approved, Approved, true, "Hi again", "", true},
		{"an approved person saved as it was", approved, Approved, true, "Hi Ada", Approved, false},
		{"an approved person, new text, category kept", approved, Approved, false, "Hi again", Pending, false},
		{"an approved person, new text, category omitted", approved, "", false, "Hi again", Pending, false},
		{"an approved person, same text, category kept", approved, Approved, false, "Hi Ada", Approved, false},
		{"queued for review", Stored{}, Pending, true, "Hi", Pending, false},
		{"rejected", approved, Rejected, true, "Hi Ada", Rejected, false},
		{"an approved person re-queued with new text", approved, Pending, true, "Hi again", Pending, false},
		{"another category", Stored{}, "lead", true, "", "lead", false},
	} {
		got, err := NotOperatorCategory(tc.stored, tc.category, tc.set, tc.intro, "an agent")
		if tc.refused {
			if !errors.Is(err, ErrNotOperator) {
				t.Errorf("%s: (%q, %v), want refused", tc.name, got, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: (%q, %v), want %q", tc.name, got, err, tc.want)
		}
	}
}
