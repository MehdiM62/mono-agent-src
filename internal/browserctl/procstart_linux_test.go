//go:build linux

package browserctl

import (
	"os"
	"strings"
	"testing"
	"time"
)

// A pid can start at the same tick after another boot: a record from
// another boot doesn't count, even when its pid and ticks match.
func TestKindRecordFromAnotherBootDoesNotCount(t *testing.T) {
	start, ok := processStart(os.Getpid())
	boot, ticks, found := strings.Cut(start, ":")
	if !ok || !found || len(boot) < 32 || ticks == "" {
		t.Fatalf("start = %q %v", start, ok)
	}
	rec := kindRecord{Kind: KindOrg, PID: os.Getpid(), Start: start, Created: time.Now()}
	if !rec.live() {
		t.Fatal("this boot's record")
	}
	rec.Start = "00000000-0000-0000-0000-000000000000:" + ticks
	if rec.live() {
		t.Fatal("a record from another boot counts")
	}
}
