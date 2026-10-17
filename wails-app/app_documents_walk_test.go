package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestWalkFolderFiles(t *testing.T) {
	// A dot-named chosen folder is still walked; dot entries below it are not.
	root := filepath.Join(t.TempDir(), ".chosen")
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "a")
	write("sub/b.txt", "bb")
	write(".env", "secret")
	write(".git/config", "x")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "o.txt"), []byte("o"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linkdir")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if err := os.Symlink(filepath.Join(root, "a.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}

	files, skipped, total, over := walkFolderFiles(root)
	if over {
		t.Fatal("unexpectedly over cap")
	}
	var rel []string
	for _, f := range files {
		r, _ := filepath.Rel(root, f)
		rel = append(rel, r)
	}
	sort.Strings(rel)
	if len(rel) != 2 || rel[0] != "a.txt" || rel[1] != filepath.Join("sub", "b.txt") {
		t.Fatalf("files = %v", rel)
	}
	if total != 3 {
		t.Fatalf("total = %d", total)
	}
	if len(skipped) != 4 { // .env, .git, linkdir, link.txt
		t.Fatalf("skipped = %v", skipped)
	}
}
