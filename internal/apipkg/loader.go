package apipkg

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/monoes/mono-agent/internal/connections"
)

// catalogFS holds the built-in packages: catalog/<id>.json.
//
//go:embed catalog/*.json
var catalogFS embed.FS

// InstallDir is where user- and library-installed packages live:
// ~/.monoagent/apipkgs/<id>/package.json.
func InstallDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return filepath.Join(home, ".monoagent", "apipkgs")
}

// loaded is the cached result of Load for one install dir.
type loaded struct {
	dir  string
	pkgs []*Package
	err  error
}

var (
	cacheMu sync.Mutex
	cache   *loaded
)

// Load returns the built-in catalog plus the installed packages, each
// validated. Invalid files are left out and reported in the error (the
// valid packages are still returned). An installed package with a catalog
// package's id replaces it. Results are cached per install dir; Reload
// drops the cache.
func Load() ([]*Package, error) {
	dir := InstallDir()
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cache != nil && cache.dir == dir {
		return cache.pkgs, cache.err
	}
	pkgs, err := loadAll(dir)
	cache = &loaded{dir: dir, pkgs: pkgs, err: err}
	return pkgs, err
}

// Reload drops the cache so the next Load reads the files again.
func Reload() {
	cacheMu.Lock()
	cache = nil
	cacheMu.Unlock()
	connections.AddPlatformSource(registerPlatforms)
}

// packages is Load without the error, for lookups.
func packages() []*Package {
	pkgs, _ := Load()
	return pkgs
}

func loadAll(dir string) ([]*Package, error) {
	var errs []error
	byID := map[string]*Package{}
	var order []string
	put := func(p *Package) {
		if _, ok := byID[p.ID]; !ok {
			order = append(order, p.ID)
		}
		byID[p.ID] = p
	}

	cat, err := CatalogFiles()
	if err != nil {
		errs = append(errs, err)
	}
	for _, name := range cat {
		b, err := catalogFS.ReadFile("catalog/" + name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		p, err := Parse(b, "catalog/"+name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if want := strings.TrimSuffix(name, ".json"); p.ID != want {
			errs = append(errs, fmt.Errorf("apipkg: catalog/%s: id %q must match the file name %q", name, p.ID, want))
			continue
		}
		p.Source = "catalog"
		put(p)
	}

	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, fmt.Errorf("apipkg: read %s: %w", dir, err))
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "package.json")
		b, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, fmt.Errorf("apipkg: %w", err))
			}
			continue
		}
		p, err := Parse(b, path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		put(p)
	}

	// Two packages may not claim one node type or extend one native node.
	var out []*Package
	claimed := map[string]string{}
	for _, id := range order {
		p := byID[id]
		t := p.TargetType()
		if other, ok := claimed[t]; ok {
			errs = append(errs, fmt.Errorf("apipkg: %s (%s) and %s both claim %s; %s is left out", other, byID[other].Source, id, t, id))
			continue
		}
		claimed[t] = id
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, errors.Join(errs...)
}

// CatalogFiles lists the built-in package files (names only).
func CatalogFiles() ([]string, error) {
	entries, err := catalogFS.ReadDir("catalog")
	if err != nil {
		return nil, fmt.Errorf("apipkg: read catalog: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// ByID returns the loaded package with id.
func ByID(id string) (*Package, bool) {
	for _, p := range packages() {
		if p.ID == id {
			return p, true
		}
	}
	return nil, false
}

// ForNodeType returns the package registering nodeType (not extensions).
func ForNodeType(nodeType string) (*Package, bool) {
	for _, p := range packages() {
		if p.Extends == "" && p.NodeTypeOf() == nodeType {
			return p, true
		}
	}
	return nil, false
}

// extensionFor returns the package extending nativeType, or nil.
func extensionFor(nativeType string) *Package {
	for _, p := range packages() {
		if p.Extends == nativeType {
			return p
		}
	}
	return nil
}
