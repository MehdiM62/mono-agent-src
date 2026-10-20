package automation

import (
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/monoes/mono-agent/automations"
)

var updateOfficialHashes = flag.Bool("update", false, "append the hashes of the automations/ sources to official_hashes.json")

func sourceHashes(t *testing.T) map[string]string {
	t.Helper()
	seeds, _, err := loadSeeds(automations.Tree())
	if err != nil || len(seeds) != len(automations.IDs) {
		t.Fatalf("loadSeeds: %d seeds, %v", len(seeds), err)
	}
	out := map[string]string{}
	for _, s := range seeds {
		out[s.pkg.Manifest.ID] = s.hash
	}
	return out
}

// TestOfficialHashesCoverSources fails until official_hashes.json lists the
// hash of every package under automations/ (run with -update to append).
func TestOfficialHashesCoverSources(t *testing.T) {
	src := sourceHashes(t)
	if *updateOfficialHashes {
		raw := map[string][]string{}
		for id, hs := range officialHashes {
			for h := range hs {
				raw[id] = append(raw[id], h)
			}
		}
		for id, h := range src {
			if !isOfficialHash(id, h) {
				raw[id] = append(raw[id], h)
			}
		}
		for id := range raw {
			sort.Strings(raw[id])
		}
		b, _ := json.MarshalIndent(raw, "", "  ")
		if err := os.WriteFile("official_hashes.json", append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	for id, h := range src {
		if !isOfficialHash(id, h) {
			t.Errorf("package %s (%s) is not in official_hashes.json: run go test ./internal/automation -run TestOfficialHashes -update", id, h)
		}
	}
}

func seededOfficial(t *testing.T) *Registry {
	t.Helper()
	r := newReg(t)
	if _, err := r.SeedWithReport(automations.Tree()); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestOfficialBuiltinKeepsItsTrust(t *testing.T) {
	r := seededOfficial(t)
	p, err := r.Get("hackernews")
	if err != nil || p.Trust != TrustBuiltin {
		t.Fatalf("trust = %q, %v", p.Trust, err)
	}
	if info, _ := r.Info("hackernews"); info.Trust != TrustBuiltin {
		t.Fatalf("info trust = %q", info.Trust)
	}
}

// An agent that rewrites an action file of a built-in, leaving the index
// saying "builtin", does not get a built-in package.
func TestEditedBuiltinFilesAreNotBuiltin(t *testing.T) {
	r := seededOfficial(t)
	info, _ := r.Info("hackernews")
	f := filepath.Join(info.Dir, "automation.json")
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, append(b, ' '), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := r.Get("hackernews")
	if err != nil || p.Trust != TrustImported {
		t.Fatalf("trust = %q, %v; want imported", p.Trust, err)
	}
	if got, _ := r.Info("hackernews"); got.Trust != TrustImported || got.ScriptsAllowed {
		t.Fatalf("info = trust %q scripts %v", got.Trust, got.ScriptsAllowed)
	}
	if rows, _ := r.List(false); len(rows) > 0 {
		for _, row := range rows {
			if row.ID == "hackernews" && row.Trust != TrustImported {
				t.Fatalf("list trust = %q", row.Trust)
			}
		}
	}
}

// A package of the agent's own, filed in the index as a built-in, is not.
func TestUnlistedPackageClaimingBuiltinIsNotBuiltin(t *testing.T) {
	r := newReg(t)
	if _, err := r.SeedWithReport(seedFS("1.0.0", "x")); err != nil {
		t.Fatal(err)
	}
	p, err := r.Get("demo")
	if err != nil || p.Source != SourceBuiltin || p.Trust != TrustImported {
		t.Fatalf("source %q trust %q, %v", p.Source, p.Trust, err)
	}
}

// officialForTest lists hash as an official release of id until the test ends,
// for the tests whose synthetic packages stand in for official ones.
func officialForTest(t *testing.T, id, hash string) {
	t.Helper()
	if officialHashes[id] == nil {
		officialHashes[id] = map[string]bool{}
	}
	officialHashes[id][hash] = true
	t.Cleanup(func() {
		delete(officialHashes[id], hash)
		if len(officialHashes[id]) == 0 {
			delete(officialHashes, id)
		}
	})
}

func seedHashForTest(t *testing.T, id string, seeds fs.FS) string {
	t.Helper()
	loaded, _, err := loadSeeds(seeds)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range loaded {
		if s.pkg.Manifest.ID == id {
			return s.hash
		}
	}
	t.Fatalf("no seed %s", id)
	return ""
}
