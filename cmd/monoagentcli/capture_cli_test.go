package main

import (
	"testing"
)

// The capture commands' tests live in internal/cli/capture; these are the wiring test and the
// helpers other commands' tests share.

// TestCaptureCommandIsRegistered guards the wiring: a command that exists
// but is never added to the root tree is invisible to every caller.
func TestCaptureCommandIsRegistered(t *testing.T) {
	root := newRootCmd()
	for _, path := range [][]string{{"capture"}, {"capture", "page"}, {"capture", "list"}, {"capture", "classify"}, {"capture", "export"}, {"capture", "import"}, {"capture", "task"}} {
		sub, _, err := root.Find(path)
		if err != nil || sub == nil || sub.Name() != path[len(path)-1] {
			t.Fatalf("root.Find(%v) = %v, %v", path, sub, err)
		}
	}
}
