package n8nimport

import (
	"regexp"
	"sort"
	"strconv"
)

// versionKeyRE matches a version_defaults key: an exact typeVersion ("2",
// "2.1") or a comparison ("<3", "<=2.2", ">=2", ">1").
var versionKeyRE = regexp.MustCompile(`^(<=|>=|<|>)?\s*([0-9]+(?:\.[0-9]+)?)$`)

// versionDefaults merges the defaults that apply to typeVersion v:
// comparison keys first (in key order), then the exact key, which wins
// (Notion before version 3 folds property keys to ASCII: "<3").
func versionDefaults(m map[string]map[string]any, v float64) map[string]any {
	exact := strconv.FormatFloat(v, 'f', -1, 64)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := map[string]any{}
	for _, k := range keys {
		mt := versionKeyRE.FindStringSubmatch(k)
		if mt == nil || mt[1] == "" {
			continue
		}
		n, err := strconv.ParseFloat(mt[2], 64)
		if err != nil {
			continue
		}
		var ok bool
		switch mt[1] {
		case "<":
			ok = v < n
		case "<=":
			ok = v <= n
		case ">":
			ok = v > n
		case ">=":
			ok = v >= n
		}
		if ok {
			for p, x := range m[k] {
				out[p] = x
			}
		}
	}
	for p, x := range m[exact] {
		out[p] = x
	}
	return out
}
