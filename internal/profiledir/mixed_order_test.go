package profiledir

import (
	"context"
	"strings"
	"testing"
)

// Two stored created_at formats, same second: Candidates and Describe order
// them by time then id, as List does.
func TestCandidatesAndDescribeOrderMixedCreatedFormats(t *testing.T) {
	db := resolveFixture(t,
		[3]string{"z", "Work", "2026-10-16 06:04:33"},
		[3]string{"a", "work", "2026-10-16T06:04:33Z"},
		[3]string{"m", "WORK", "2026-10-16T06:04:32Z"},
	)
	ids, err := Candidates(context.Background(), db, "work")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ids, ""); got != "maz" {
		t.Fatalf("Candidates order = %q, want maz", got)
	}
	if got := Describe(context.Background(), db); got != "WORK (m), work (a), Work (z)" {
		t.Fatalf("Describe = %q", got)
	}
}
