package n8nimport

import (
	"strings"
	"testing"
)

// WithHandshakeKey makes the importer's new Wufoo handshake keys key (for
// the external tests); the returned func restores the generator.
func WithHandshakeKey(key string) func() {
	old := newHandshakeKey
	newHandshakeKey = func() string { return key }
	return func() { newHandshakeKey = old }
}

// Version defaults may name a comparison: "<3" applies to every older
// typeVersion, and an exact key wins over it.
func TestVersionDefaultsRanges(t *testing.T) {
	m := map[string]map[string]any{
		"<3":   {"keyCase": "ascii", "x": "old"},
		">=2":  {"y": "new"},
		"2.2":  {"x": "exact"},
		"bad!": {"z": 1},
	}
	cases := []struct {
		v    float64
		want string
	}{
		{1, "keyCase=ascii x=old"},
		{2, "keyCase=ascii x=old y=new"},
		{2.2, "keyCase=ascii x=exact y=new"},
		{3, "y=new"},
	}
	for _, c := range cases {
		got := versionDefaults(m, c.v)
		var parts []string
		for _, k := range sortedKeys(got) {
			parts = append(parts, k+"="+got[k].(string))
		}
		if s := strings.Join(parts, " "); s != c.want {
			t.Errorf("v%v: %s, want %s", c.v, s, c.want)
		}
	}
}
