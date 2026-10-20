package automation

import (
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
		raw := map[string]officialEntry{}
		for id, hs := range officialHashes {
			raw[id] = officialEntry{MinVersion: officialMin[id], Hashes: hs}
		}
		seeds, _, _ := loadSeeds(automations.Tree())
		for _, s := range seeds {
			id := s.pkg.Manifest.ID
			if isOfficialRelease(id, s.pkg.Manifest.Version, s.hash) {
				continue
			}
			e := raw[id]
			if e.Hashes == nil {
				e.Hashes = map[string]string{}
			}
			e.Hashes[s.hash] = s.pkg.Manifest.Version
			raw[id] = e
		}
		b, _ := json.MarshalIndent(raw, "", "  ")
		if err := os.WriteFile("official_hashes.json", append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	vers := map[string]string{}
	seeds, _, _ := loadSeeds(automations.Tree())
	for _, s := range seeds {
		vers[s.pkg.Manifest.ID] = s.pkg.Manifest.Version
	}
	for id, h := range src {
		if !isOfficialRelease(id, vers[id], h) {
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
		officialHashes[id] = map[string]string{}
	}
	officialHashes[id][hash] = "*"
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

func hackernewsDir(t *testing.T, r *Registry) string {
	t.Helper()
	info, err := r.Info("hackernews")
	if err != nil {
		t.Fatal(err)
	}
	return info.Dir
}

// A symlink, a dotfile or a special file added inside a genuine built-in's
// version dir is read at run time (os.DirFS follows links) without being
// hashed, so the package does not keep the built-in tier.
func TestBuiltinWithExtraTreeEntriesIsNotBuiltin(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"symlink": func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(os.DevNull, filepath.Join(dir, "scripts", "x.js")); err != nil {
				t.Skip("no symlinks:", err)
			}
		},
		"dotfile": func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"dotdir": func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "scripts", ".d"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "scripts", ".d", "a.js"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, add := range cases {
		t.Run(name, func(t *testing.T) {
			r := seededOfficial(t)
			add(t, hackernewsDir(t, r))
			p, err := r.Get("hackernews")
			if err != nil || p.Trust != TrustImported {
				t.Fatalf("trust = %q, %v; want imported", p.Trust, err)
			}
		})
	}
}

// The files a built-in run reads are the ones that were hashed: a file
// swapped after Get does not reach the running package.
func TestBuiltinGetServesTheVerifiedSnapshot(t *testing.T) {
	r := seededOfficial(t)
	p, err := r.Get("hackernews")
	if err != nil || p.Trust != TrustBuiltin {
		t.Fatalf("trust = %q, %v", p.Trust, err)
	}
	before, err := fs.ReadFile(p.FS, ManifestFile)
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(hackernewsDir(t, r), ManifestFile)
	if err := os.WriteFile(f, append(append([]byte{}, before...), []byte("\n/* swapped */")...), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := fs.ReadFile(p.FS, ManifestFile)
	if err != nil || string(after) != string(before) {
		t.Fatalf("package read the swapped file: %v", err)
	}
}

// An old official release is not built-in again once the shipped minimum
// for its package has moved past it, and a hash filed for one version does
// not vouch for a tree that says another.
func TestOfficialHashIsPerVersionWithAMinimum(t *testing.T) {
	r := seededOfficial(t)
	p, _ := r.Get("hackernews")
	h, err := fsHash(p.FS)
	if err != nil {
		t.Fatal(err)
	}
	v := p.Manifest.Version
	if !isOfficialRelease("hackernews", v, h) {
		t.Fatal("the shipped release is not official")
	}
	if isOfficialRelease("hackernews", "0.0.1", h) {
		t.Fatal("a hash vouched for another version")
	}
	old := officialMin["hackernews"]
	officialMin["hackernews"] = "99.0.0"
	t.Cleanup(func() { officialMin["hackernews"] = old })
	if isOfficialRelease("hackernews", v, h) {
		t.Fatal("a release below the minimum is still official")
	}
	if got, _ := r.Get("hackernews"); got.Trust != TrustImported {
		t.Fatalf("trust = %q below the minimum", got.Trust)
	}
}

func TestUnshippedOfficialWarning(t *testing.T) {
	r := seededOfficial(t)
	if w := r.UnshippedOfficialWarning("hackernews"); w != "" {
		t.Fatalf("shipped release warned: %s", w)
	}
	f := filepath.Join(hackernewsDir(t, r), "automation.json")
	b, _ := os.ReadFile(f)
	if err := os.WriteFile(f, append(b, ' '), 0o644); err != nil {
		t.Fatal(err)
	}
	if w := r.UnshippedOfficialWarning("hackernews"); !strings.Contains(w, "update the app") {
		t.Fatalf("warning = %q", w)
	}
}
