package extension

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An image is read with one open and a cap on the bytes read, not only on
// what a stat said.
func TestReadImageFileCap(t *testing.T) {
	dir := t.TempDir()
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDRfake")
	small, big := filepath.Join(dir, "small.png"), filepath.Join(dir, "big.png")
	if err := os.WriteFile(small, png, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(big, append(png, make([]byte, 100)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, ok := readImageFile(small, 100); !ok || len(b) != len(png) {
		t.Errorf("small: %d bytes, ok=%v", len(b), ok)
	}
	if _, ok := readImageFile(big, 100); ok {
		t.Error("read a file over the cap")
	}
	if _, ok := readImageFile(dir, 100); ok {
		t.Error("read a folder")
	}
	if _, ok := readAtMost(strings.NewReader(strings.Repeat("x", 101)), 100); ok {
		t.Error("a reader past the cap was accepted")
	}
	if b, ok := readAtMost(strings.NewReader(strings.Repeat("x", 100)), 100); !ok || len(b) != 100 {
		t.Errorf("a reader at the cap: %d, ok=%v", len(b), ok)
	}
}
