package extensioncli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/testdb"
	"github.com/monoes/mono-agent/internal/vault"
)

// vaultEnv is a migrated database with two live profiles whose folders are
// under a scratch HOME, so no save can reach the real one.
func vaultEnv(t *testing.T) (path, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path = testdb.Path(t)
	db, err := storage.NewDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, id := range []string{"p-work", "p-home"} {
		if _, err := db.DB.Exec(`INSERT INTO profiles (id, name) VALUES (?, ?)`, id, id); err != nil {
			t.Fatal(err)
		}
	}
	return path, home
}

func vaultFile(profile, clientID string) extension.VaultFile {
	data := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("x"), 100)...)
	return extension.VaultFile{ProfileID: profile, ClientID: clientID, Name: "invoice.pdf", MIME: "application/pdf", Ext: ".pdf",
		Data: data, SourceURL: "https://example.test/invoice.pdf"}
}

type vaultRow struct {
	id, path, filename, source, label, clientID, sourceURL string
	size                                                   int
}

func vaultRows(t *testing.T, path string) []vaultRow {
	t.Helper()
	db, err := storage.NewDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.DB.Query(`SELECT id, path, filename, source, COALESCE(label,''), COALESCE(client_id,''), COALESCE(source_url,''), size_bytes
		FROM vault_images ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []vaultRow
	for rows.Next() {
		var r vaultRow
		if err := rows.Scan(&r.id, &r.path, &r.filename, &r.source, &r.label, &r.clientID, &r.sourceURL, &r.size); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// filesIn lists the regular files under dir (none when it does not exist).
func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// One save is one file and one row in the profile's image/file vault, the
// vault vault_item_list and vault_item_get_path read; a retry of the same
// client id writes nothing and returns the same item.
func TestVaultSinkSavesOnceIntoTheProfilesVault(t *testing.T) {
	path, home := vaultEnv(t)
	f := vaultFile("p-work", "v-1")
	got, err := extensionVaultSink(path).SaveToVault(context.Background(), f)
	if err != nil || !got.Created || got.ID == "" || got.Name != "invoice.pdf" || got.MIME != "application/pdf" || got.SizeBytes != len(f.Data) {
		t.Fatalf("save: %+v, %v", got, err)
	}
	rows := vaultRows(t, path)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	r := rows[0]
	vaultDir := filepath.Join(home, ".monoagent", "profiles", "p-work", ".monoagent", "vault")
	if r.id != got.ID || r.path != filepath.Join(vaultDir, got.ID+".pdf") || r.filename != got.ID+".pdf" || r.source != "extension" ||
		r.label != "invoice.pdf" || r.clientID != "v-1" || r.sourceURL != "https://example.test/invoice.pdf" || r.size != len(f.Data) {
		t.Errorf("row %+v", r)
	}
	if b, err := os.ReadFile(r.path); err != nil || !bytes.Equal(b, f.Data) {
		t.Errorf("stored file: %v", err)
	}
	if fi, err := os.Stat(r.path); err == nil && fi.Mode().Perm() != 0o600 {
		t.Errorf("stored file mode %v, want 0600", fi.Mode().Perm())
	}

	// vault_item_get_path resolves the id the reply named.
	db, _ := storage.NewDatabase(path)
	resolved, err := vault.Resolve(vault.ContextWithProfileID(context.Background(), "p-work"), db.DB, "@"+got.ID)
	db.Close()
	if err != nil || resolved != r.path {
		t.Errorf("vault.Resolve = %q, %v", resolved, err)
	}

	again, err := extensionVaultSink(path).SaveToVault(context.Background(), f)
	if err != nil || again.Created || again.ID != got.ID {
		t.Errorf("retry: %+v, %v", again, err)
	}
	if n := len(vaultRows(t, path)); n != 1 {
		t.Errorf("rows after the retry = %d, want 1", n)
	}
	if files := filesIn(t, vaultDir); len(files) != 1 {
		t.Errorf("files after the retry = %v, want one", files)
	}

	// The client id is per profile: another profile's save is its own item.
	other, err := extensionVaultSink(path).SaveToVault(context.Background(), vaultFile("p-home", "v-1"))
	if err != nil || !other.Created || other.ID == got.ID {
		t.Errorf("another profile, same client id: %+v, %v", other, err)
	}
}

// Many sends of one client id at once still make one item.
func TestVaultSinkIsIdempotentUnderConcurrency(t *testing.T) {
	path, home := vaultEnv(t)
	var wg sync.WaitGroup
	ids := make([]string, 8)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := extensionVaultSink(path).SaveToVault(context.Background(), vaultFile("p-work", "v-same"))
			if err != nil {
				t.Errorf("save %d: %v", i, err)
			}
			ids[i] = got.ID
		}(i)
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Errorf("ids = %v, want one id", ids)
			break
		}
	}
	if n := len(vaultRows(t, path)); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
	if files := filesIn(t, filepath.Join(home, ".monoagent", "profiles", "p-work")); len(files) != 1 {
		t.Errorf("files = %v, want one", files)
	}
}

// A profile that is not in the database (deleted since the panel listed it, or
// never there) is refused, and gets no folder and no row.
func TestVaultSinkRefusesADeadProfile(t *testing.T) {
	path, home := vaultEnv(t)
	_, err := extensionVaultSink(path).SaveToVault(context.Background(), vaultFile("p-gone", "v-1"))
	if err == nil || sinkCode(t, err) != extension.CodeInvalidInput {
		t.Fatalf("dead profile: %v, want invalid_input", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".monoagent", "profiles", "p-gone")); !os.IsNotExist(err) {
		t.Errorf("the dead profile's folder was created: %v", err)
	}
	if n := len(vaultRows(t, path)); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

// No database, or one from before vault.save: unavailable, nothing written,
// and the bridge never migrates or creates the database.
func TestVaultSinkWithoutAReadyDatabase(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	missing := filepath.Join(t.TempDir(), "none.db")
	if _, err := extensionVaultSink(missing).SaveToVault(context.Background(), vaultFile("p-work", "v-1")); err == nil || sinkCode(t, err) != extension.CodeUnavailable {
		t.Errorf("no database: %v, want unavailable", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("the database was created: %v", err)
	}

	path, _ := vaultEnv(t)
	db, err := storage.NewDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DROP INDEX idx_vault_images_client`, `ALTER TABLE vault_images DROP COLUMN client_id`} {
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	if _, err := extensionVaultSink(path).SaveToVault(context.Background(), vaultFile("p-work", "v-1")); err == nil || sinkCode(t, err) != extension.CodeUnavailable {
		t.Errorf("an old database: %v, want unavailable", err)
	}
}

// Every bridge this binary builds answers vault.save, and none has a method
// that reads the vault.
func TestEveryBridgeAnswersVaultSaveAndNothingReadsTheVault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := newExtensionServer(zerolog.Nop(), "test")
	found := false
	for _, m := range srv.RequestMethods() {
		switch {
		case m == extension.MethodVaultSave:
			found = true
		case len(m) > 6 && m[:6] == "vault.":
			t.Errorf("the bridge has another vault method: %s", m)
		}
	}
	if !found {
		t.Fatalf("%s is not advertised by the bridge newExtensionServer builds", extension.MethodVaultSave)
	}
}
