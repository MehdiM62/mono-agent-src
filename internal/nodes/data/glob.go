package data

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/monoes/mono-agent/internal/fsconfine"
)

// File selection for data.read_write_file: filepath.Match patterns per path
// segment, plus "**" (any number of directories) and {a,b} alternatives.
// As in n8n, "*" and "**" do not match names starting with a dot unless the
// pattern segment itself starts with one.
//
// Confinement: the fixed directory the pattern starts from goes through
// fsconfine.Path before anything is listed, and so does every match before
// it is returned — a symlink inside the workdir that points outside it is
// refused, not followed.

// globFiles returns the regular files matching pattern, sorted, as the
// confined (resolved) paths to open.
func globFiles(ctx context.Context, pattern string, literalBrackets bool) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, p := range expandBraces(filepath.FromSlash(pattern)) {
		matches, err := globOne(ctx, p, literalBrackets)
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func globOne(ctx context.Context, pattern string, literalBrackets bool) ([]string, error) {
	sep := string(filepath.Separator)
	segs := strings.Split(pattern, sep)
	fixed := 0
	for fixed < len(segs) && !hasGlobMeta(segs[fixed], literalBrackets) {
		fixed++
	}
	root := strings.Join(segs[:fixed], sep)
	if root == "" && filepath.IsAbs(pattern) {
		root = sep
	}
	if root == "" {
		root = "."
	}
	resolvedRoot, err := fsconfine.Path(ctx, root)
	if err != nil {
		return nil, err
	}
	if abs, err := filepath.Abs(resolvedRoot); err == nil {
		resolvedRoot = abs
	}
	rest := segs[fixed:]
	if len(rest) == 0 {
		return confinedRegular(ctx, resolvedRoot)
	}
	for i, s := range rest {
		if literalBrackets {
			rest[i] = strings.NewReplacer("[", `\[`, "]", `\]`).Replace(s)
		}
	}
	deep := false
	for _, s := range rest {
		if s == "**" {
			deep = true
		}
	}
	var out []string
	err = filepath.WalkDir(resolvedRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == resolvedRoot {
				return filepath.SkipDir
			}
			return nil // unreadable entry: not a match
		}
		if path == resolvedRoot {
			return nil
		}
		rel, _ := filepath.Rel(resolvedRoot, path)
		parts := strings.Split(rel, sep)
		if d.IsDir() {
			if !deep && len(parts) >= len(rest) {
				return filepath.SkipDir
			}
			return nil
		}
		if !matchSegments(rest, parts) {
			return nil
		}
		files, err := confinedRegular(ctx, path)
		if err != nil {
			return err
		}
		out = append(out, files...)
		return nil
	})
	return out, err
}

// confinedRegular passes p through fsconfine and returns it when it is a
// regular file (symlinks followed).
func confinedRegular(ctx context.Context, p string) ([]string, error) {
	resolved, err := fsconfine.Path(ctx, p)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(resolved)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, nil
	}
	return []string{resolved}, nil
}

func hasGlobMeta(seg string, literalBrackets bool) bool {
	if literalBrackets {
		return strings.ContainsAny(seg, "*?")
	}
	return strings.ContainsAny(seg, "*?[")
}

// matchSegments matches path segments against pattern segments, "**"
// standing for zero or more whole segments.
func matchSegments(pat, parts []string) bool {
	if len(pat) == 0 {
		return len(parts) == 0
	}
	if pat[0] == "**" {
		if matchSegments(pat[1:], parts) {
			return true
		}
		return len(parts) > 0 && !strings.HasPrefix(parts[0], ".") && matchSegments(pat, parts[1:])
	}
	if len(parts) == 0 {
		return false
	}
	if strings.HasPrefix(parts[0], ".") && !strings.HasPrefix(pat[0], ".") {
		return false
	}
	ok, err := filepath.Match(pat[0], parts[0])
	return err == nil && ok && matchSegments(pat[1:], parts[1:])
}

// expandBraces expands {a,b} alternatives ("*.{png,jpg}" -> "*.png",
// "*.jpg"), nested ones included. Unbalanced braces are left as they are.
func expandBraces(p string) []string {
	open := strings.IndexByte(p, '{')
	if open < 0 {
		return []string{p}
	}
	depth := 0
	var commas []int
	for i := open; i < len(p); i++ {
		switch p[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				if len(commas) == 0 {
					var out []string
					for _, tail := range expandBraces(p[i+1:]) {
						out = append(out, p[:i+1]+tail)
					}
					return out
				}
				var out []string
				start := open + 1
				for _, c := range append(commas, i) {
					for _, alt := range expandBraces(p[:open] + p[start:c] + p[i+1:]) {
						out = append(out, alt)
					}
					start = c + 1
				}
				return out
			}
		case ',':
			if depth == 1 {
				commas = append(commas, i)
			}
		}
	}
	return []string{p}
}
