package agentusage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/testdb"
)

func TestRecordTurnRoundTrip(t *testing.T) {
	db := testdb.Open(t).DB
	ctx := context.Background()
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	res := &monomind.TurnResult{SessionID: "s", OutputTokens: 0, HasOutputTokens: true, CostUSD: 0, HasCostUSD: true}
	if err := RecordTurn(ctx, db, Turn{At: at, ProfileID: "p1", Source: SourceChat, Runtime: "codex"}, res); err != nil {
		t.Fatal(err)
	}
	if err := Record(ctx, nil, Turn{Runtime: "x"}); err != nil {
		t.Fatalf("nil db: %v", err)
	}
	turns, err := List(ctx, db, at)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("turns = %+v", turns)
	}
	got := turns[0]
	if !got.At.Equal(at) || got.ProfileID != "p1" || got.SessionID != "s" || got.Source != SourceChat {
		t.Errorf("turn = %+v", got)
	}
	// Reported zero is stored as zero, unreported as NULL.
	if got.InputTokens != nil || got.OutputTokens == nil || *got.OutputTokens != 0 || got.CostUSD == nil || *got.CostUSD != 0 {
		t.Errorf("metrics = in %v out %v cost %v", got.InputTokens, got.OutputTokens, got.CostUSD)
	}
}

func TestRecordRefusesAnEmptyProfile(t *testing.T) {
	db := testdb.Open(t).DB
	err := Record(context.Background(), db, Turn{Source: SourceChat, Runtime: "codex"})
	if !errors.Is(err, ErrProfileRequired) {
		t.Fatalf("err = %v, want ErrProfileRequired", err)
	}
	turns, _ := List(context.Background(), db, time.Time{})
	if len(turns) != 0 {
		t.Fatalf("a turn without a profile was stored: %+v", turns)
	}
}
