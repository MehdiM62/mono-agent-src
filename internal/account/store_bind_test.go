package account

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Which sealed folder a token may be read in (#878): the identity decides when both are known, the
// path only when one is not.
func TestSealedHere(t *testing.T) {
	dir, other := t.TempDir(), t.TempDir()
	id := folderID(dir)
	if id == "" {
		t.Skip("this file system gives folders no identity")
	}
	for _, c := range []struct {
		name, folder, id string
		want             bool
	}{
		{"same path, same identity", dir, id, true},
		{"same path, another identity: a copy put back where the token was sealed", dir, folderID(other), false},
		{"another path, same identity: the folder renamed", other, id, true},
		{"another path, another identity: a copy", other, folderID(other), false},
		{"same path, identity not known when sealed", dir, "", true},
		{"another path, identity not known when sealed", other, "", false},
	} {
		if got := sealedHere(c.folder, c.id, dir); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// An inode or a file id of 0 is no identity: some FUSE and network file systems give it to every
// file, which would make every folder there the same one.
func TestAZeroIdentityIsNone(t *testing.T) {
	if got := devIno(42, 0); got != "" {
		t.Errorf("inode 0: %q, want none", got)
	}
	if got := devIno(42, 7); got != "42:7" {
		t.Errorf("inode 7: %q", got)
	}
	if got := volumeFileID(9, [16]byte{}); got != "" {
		t.Errorf("file id 0: %q, want none", got)
	}
	if got := volumeFileID(9, [16]byte{15: 1}); got != "9:00000000000000000000000000000001" {
		t.Errorf("file id 1: %q", got)
	}
}

// The refusal names both folders quoted, so a path with a line break in it cannot add a line.
func TestTheRefusalQuotesItsPaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "line\nbreak")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Skip("no line break in a file name here:", err)
	}
	_, err := sealedFor(dir, []byte(sealedForPrefix+"/elsewhere\nfolder\x00"+"1:2\x00rt"))
	if err == nil || strings.Contains(err.Error(), "\n") || !strings.Contains(err.Error(), `"/elsewhere\nfolder"`) {
		t.Fatalf("%v", err)
	}
}
