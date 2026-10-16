package testhome

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// unguarded lists the test packages (directories, slash-separated, relative
// to the repository root) whose TestMain may skip Main, and why.
var unguarded = map[string]string{
	"internal/testhome": "this package: its TestMain calls Main directly",
}

// TestEveryTestPackageUsesMain fails for a test package anywhere in the
// repository (the wails-app module included) whose TestMain does not call
// testhome.Main: its tests could write into the user's real ~/.monoagent.
func TestEveryTestPackageUsesMain(t *testing.T) {
	root := repoRoot(t)
	dirs := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			dirs[filepath.Dir(path)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) < 100 {
		t.Fatalf("found only %d test packages under %s: is the walk broken?", len(dirs), root)
	}

	var missing []string
	seen := map[string]bool{}
	for dir := range dirs {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			t.Fatal(err)
		}
		rel = filepath.ToSlash(rel)
		if _, ok := unguarded[rel]; ok {
			seen[rel] = true
			continue
		}
		if !callsMain(t, dir) {
			missing = append(missing, rel)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d test package(s) run without testhome.Main, so their tests can write into the real ~/.monoagent.\n"+
			"Add a testmain_test.go like internal/cli/chat's (keyring.MockInit() too if it reaches the vault):\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
	for rel := range unguarded {
		if !seen[rel] {
			t.Errorf("unguarded lists %s, which has no tests any more: drop it", rel)
		}
	}
}

// callsMain reports whether a TestMain in dir's test files calls testhome.Main.
func callsMain(t *testing.T, dir string) bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(src, []byte("func TestMain(")) {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), f, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != "TestMain" || fn.Body == nil {
				continue
			}
			found := false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Main" {
						if x, ok := sel.X.(*ast.Ident); ok && x.Name == "testhome" {
							found = true
						}
					}
				}
				return !found
			})
			if found {
				return true
			}
		}
	}
	return false
}

// repoRoot is the directory of the go.mod above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the testhome package")
		}
		dir = parent
	}
}
