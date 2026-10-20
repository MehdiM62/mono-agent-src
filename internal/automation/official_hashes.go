package automation

import (
	_ "embed"
	"encoding/json"
	"os"
)

// Official package hashes (#900). A package whose trust tier is "builtin"
// (an embedded built-in of an older release, or an official monoes.me item)
// is trusted for what an agent may run without the user, so the tier cannot
// rest on files an agent can also write: the index and the package files
// both live under ~/.monoagent. The hashes of the official packages ship in
// the binary instead, and a package whose files do not hash to one of them
// is treated as imported, whatever its index entry says.
//
// A legitimate change to a built-in never keeps the tier: editing it lowers
// the entry's trust (lowerTrust), and selector fixes go to the overlay. So a
// "builtin" entry that fails this check was altered outside the app.
//
// official_hashes.json maps package id to the tree hashes (treeHash) of
// every official release of it. `go test ./internal/automation -run
// TestOfficialHashes -update` appends the hashes of the automations/ sources;
// TestOfficialHashesCoverSources fails until a release has done so.

//go:embed official_hashes.json
var officialHashesJSON []byte

var officialHashes = mustOfficialHashes(officialHashesJSON)

func mustOfficialHashes(b []byte) map[string]map[string]bool {
	var raw map[string][]string
	if err := json.Unmarshal(b, &raw); err != nil {
		panic("automation: official_hashes.json: " + err.Error())
	}
	out := make(map[string]map[string]bool, len(raw))
	for id, hs := range raw {
		out[id] = make(map[string]bool, len(hs))
		for _, h := range hs {
			out[id][h] = true
		}
	}
	return out
}

// isOfficialHash reports whether h is the hash of an official release of id.
func isOfficialHash(id, h string) bool { return officialHashes[id][h] }

// effectiveTrust is the trust tier e's package may act with. Anything but
// "builtin" is returned as recorded; "builtin" holds only while the
// version's files hash to an official release, else it falls to imported.
func (r *Registry) effectiveTrust(id string, e *indexEntry) string {
	t := e.trust()
	if t != TrustBuiltin {
		return t
	}
	if h, err := fsHash(os.DirFS(r.versionDir(id, e.Version))); err == nil && isOfficialHash(id, h) {
		return t
	}
	return TrustImported
}
