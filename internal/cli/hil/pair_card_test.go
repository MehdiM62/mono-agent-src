package hil

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/storage"
)

// A browser's pairing card is decided only by the operator path: `hil approve`
// approves the claim, an agent's approve changes nothing, and an approval on
// the bare row (the MCP/API route) does not approve the claim.
func TestHILPairCardDecidesTheClaim(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &testConfig{DBPath: newHILTestDB(t), ProfileID: "default"}
	claim, _, err := extension.CreatePendingClaim(extension.PendingClaim{Label: "Edge", Origin: "chrome-extension://aaaa"})
	if err != nil {
		t.Fatal(err)
	}
	sdb, err := storage.NewDatabase(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sdb.Close()
	if err := extension.OpenPairCard(context.Background(), sdb.DB, "default", claim); err != nil {
		t.Fatal(err)
	}
	var cardID, ro string
	if err := sdb.DB.QueryRow(`SELECT id, readonly_data FROM hil_pending WHERE execution_id = ?`, "extpair:"+claim.ID).Scan(&cardID, &ro); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Edge", "chrome-extension://aaaa", extension.PairClaimWording} {
		if !strings.Contains(ro, want) {
			t.Fatalf("card does not show %q: %s", want, ro)
		}
	}

	// A row updated by a route that bypasses the CLI does not approve the claim.
	if _, err := sdb.DB.Exec(`UPDATE hil_pending SET status = 'approved' WHERE id = ?`, cardID); err != nil {
		t.Fatal(err)
	}
	if left, _ := extension.ListPendingClaims(); len(left) != 1 {
		t.Fatalf("a bare row update decided the claim: %+v", left)
	}
	if _, err := sdb.DB.Exec(`UPDATE hil_pending SET status = 'pending' WHERE id = ?`, cardID); err != nil {
		t.Fatal(err)
	}

	deps := testDeps(cfg)
	deps.RequireOperator = func(string) error { return errors.New("operator only") }
	cmd := NewCommand(deps)
	cmd.SetArgs([]string{"approve", cardID})
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	if err := cmd.Execute(); err == nil {
		t.Fatal("an agent approved a pairing card")
	}
	if left, _ := extension.ListPendingClaims(); len(left) != 1 {
		t.Fatalf("a refused approve changed the claim: %+v", left)
	}

	deps = testDeps(cfg)
	deps.RequireOperator = func(string) error { return nil }
	out := captureStdout(t, func() {
		cmd := NewCommand(deps)
		cmd.SetArgs([]string{"approve", cardID})
		err = cmd.Execute()
	})
	if err != nil || !strings.Contains(out, "Approved HIL item") {
		t.Fatalf("approve = %v\n%s", err, out)
	}
	var status string
	_ = sdb.DB.QueryRow(`SELECT status FROM hil_pending WHERE id = ?`, cardID).Scan(&status)
	if status != "approved" {
		t.Fatalf("card status = %q", status)
	}
	if left, _ := extension.ListPendingClaims(); len(left) != 0 {
		t.Fatalf("claim still waiting after approval: %+v", left)
	}
	c, err := extension.DecidePendingClaim(claim.ID, true)
	_ = c
	if err == nil {
		t.Fatal("the claim should be decided already")
	}
}
