package extension

import "testing"

func TestCurrentExePath(t *testing.T) {
	has := func(set ...string) func(string) bool {
		return func(p string) bool {
			for _, s := range set {
				if s == p {
					return true
				}
			}
			return false
		}
	}
	const live = "/home/u/.local/share/monoagent/monoagentcli"
	cases := []struct {
		name, exe string
		exists    func(string) bool
		want      string
	}{
		{"untouched", live, has(live), live},
		{"renamed by update", live + ".bak", has(live), live},
		{"renamed then deleted", live + ".bak (deleted)", has(live), live},
		{"original gone keeps reported path", live + ".bak", has(), live + ".bak"},
		{"plain deleted", live + " (deleted)", has(live), live},
	}
	for _, c := range cases {
		if got := currentExePath(c.exe, c.exists); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
