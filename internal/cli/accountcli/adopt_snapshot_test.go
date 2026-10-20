package accountcli

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/account/accounttest"
	"github.com/monoes/mono-agent/internal/library"
	"github.com/monoes/mono-agent/internal/storage"
)

// The first-run adoption changes the database (the try is claimed, an adopted library secret is
// removed), so a snapshot of the state before this binary touched anything is taken first (#877).
func TestAdoptFirstRunSnapshotsBeforeChangingAnything(t *testing.T) {
	accounttest.Install(t, accounttest.LockedNoLogin)
	account.SetEnforceFromForTest(t, time.Now().Add(48*time.Hour))
	path := ownDB(t)
	seed, err := storage.NewDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.DB.Exec(`INSERT INTO vault_secrets (id, seq, profile_id, kind, name, ciphertext, nonce, created_at, updated_at)
		VALUES ('s1', 1, 'default', 'library', 'library-login', x'00', x'00', 'now', 'now')`); err != nil {
		t.Fatal(err)
	}
	seed.Close()

	removing := func(ctx context.Context, db *sql.DB, _ *account.Guard, _ func(string, library.Outcome)) (bool, error) {
		_, err := db.ExecContext(ctx, `DELETE FROM vault_secrets WHERE id = 's1'`)
		return true, err
	}
	AdoptFirstRun(find(t, "workflow", "list"), path, removing)

	snaps, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "backups", "pre-migrate-*.db"))
	if len(snaps) != 1 {
		t.Fatalf("%d snapshots, want 1: %v", len(snaps), snaps)
	}
	snap, err := sql.Open("sqlite", "file:"+snaps[0]+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	var secrets, adopted int
	if err := snap.QueryRow(`SELECT COUNT(*) FROM vault_secrets WHERE id = 's1'`).Scan(&secrets); err != nil {
		t.Fatal(err)
	}
	if err := snap.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = ?`, AdoptionSetting).Scan(&adopted); err != nil {
		t.Fatal(err)
	}
	if secrets != 1 || adopted != 0 {
		t.Errorf("snapshot holds %d library secrets and %d account_adoption rows, want 1 and 0", secrets, adopted)
	}
}
