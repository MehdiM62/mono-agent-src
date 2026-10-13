package agentusage

import (
	"context"
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
	if err := RecordTurn(ctx, db, Turn{At: at, Source: SourceChat, Runtime: "codex"}, res); err != nil {
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
	if !got.At.Equal(at) || got.ProfileID != "default" || got.SessionID != "s" || got.Source != SourceChat {
		t.Errorf("turn = %+v", got)
	}
	// Reported zero is stored as zero, unreported as NULL.
	if got.InputTokens != nil || got.OutputTokens == nil || *got.OutputTokens != 0 || got.CostUSD == nil || *got.CostUSD != 0 {
		t.Errorf("metrics = in %v out %v cost %v", got.InputTokens, got.OutputTokens, got.CostUSD)
	}
}
