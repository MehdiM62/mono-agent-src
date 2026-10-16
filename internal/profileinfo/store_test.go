package profileinfo

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/storage"
)

var bg = context.Background()

// newDB is a migrated database with one profile, "p1", whose folder is a
// temp dir, so nothing here touches the real home.
func newDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	keyring.MockInit()
	db, err := storage.NewDatabase(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Close() })
	root := t.TempDir()
	if _, err := db.DB.Exec(`INSERT INTO profiles (id, name, root_dir) VALUES ('p1', 'one', ?)`, root); err != nil {
		t.Fatal(err)
	}
	return db.DB, root
}

func TestCatalogKeysAreUniqueAndGrouped(t *testing.T) {
	groups := map[string]bool{}
	for _, g := range Groups {
		groups[g.ID] = true
	}
	seen := map[string]bool{}
	for _, fd := range Catalog {
		if seen[fd.Key] {
			t.Errorf("duplicate key %s", fd.Key)
		}
		seen[fd.Key] = true
		if !groups[fd.Group] {
			t.Errorf("%s: unknown group %s", fd.Key, fd.Group)
		}
		if fd.Kind == Select && len(fd.Options) == 0 {
			t.Errorf("%s: a select without options", fd.Key)
		}
		if (fd.Group == "card") != fd.Card {
			t.Errorf("%s: Card flag and card group disagree", fd.Key)
		}
	}
}

func TestSetGetValidatesAndKeepsCatalogOrder(t *testing.T) {
	db, _ := newDB(t)
	for k, v := range map[string]string{"last_name": " Lovelace ", "first_name": "Ada", "title": "ms.", "date_of_birth": "1815-12-10"} {
		if err := Set(bg, db, "p1", k, v, SourceUser, ""); err != nil {
			t.Fatalf("%s: %v", k, err)
		}
	}
	got, err := Get(bg, db, "p1", false)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, e := range got {
		keys = append(keys, e.Key+"="+e.Value)
	}
	if want := "title=Ms.,first_name=Ada,last_name=Lovelace,date_of_birth=1815-12-10"; strings.Join(keys, ",") != want {
		t.Errorf("got %v", keys)
	}
	for k, v := range map[string]string{"title": "Emperor", "date_of_birth": "10/12/1815", "nope": "x", "first_name": strings.Repeat("a", 301)} {
		if err := Set(bg, db, "p1", k, v, SourceUser, ""); err == nil {
			t.Errorf("%s was accepted", k)
		}
	}
	if err := Set(bg, db, "p1", "first_name", "", SourceUser, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := Get(bg, db, "p1", false); len(got) != 3 {
		t.Errorf("an empty value should remove the field: %d left", len(got))
	}
}

func TestSensitiveAndCardAreEncryptedAtRestAndMasked(t *testing.T) {
	db, _ := newDB(t)
	for k, v := range map[string]string{"passport_number": "X1234567", "card_number": "4242 4242 4242 4242"} {
		if err := Set(bg, db, "p1", k, v, SourceUser, ""); err != nil {
			t.Fatal(err)
		}
	}
	var raw string
	if err := db.QueryRow(`SELECT value FROM profile_info WHERE key = 'passport_number'`).Scan(&raw); err != nil || strings.Contains(raw, "X1234567") || !strings.HasPrefix(raw, "vaultenc:") {
		t.Errorf("stored as %q, err %v", raw, err)
	}
	masked, _ := Get(bg, db, "p1", false)
	if len(masked) != 2 || masked[0].Value != "•••• 4567" || masked[1].Value != "•••• 4242" || !masked[0].Masked || !masked[1].Masked {
		t.Errorf("masked: %+v", masked)
	}
	plain, _ := Get(bg, db, "p1", true)
	if plain[0].Value != "X1234567" || plain[1].Value != "4242 4242 4242 4242" || plain[0].Masked {
		t.Errorf("reveal: %+v", plain)
	}
}

func TestTheAINeverReplacesWhatThePersonTypedOrTouchesTheCard(t *testing.T) {
	db, _ := newDB(t)
	if err := Set(bg, db, "p1", "first_name", "Ada", SourceUser, ""); err != nil {
		t.Fatal(err)
	}
	if err := Set(bg, db, "p1", "first_name", "Augusta", SourceAI, "cv.pdf"); !errors.Is(err, ErrUserOwned) {
		t.Errorf("AI over user: %v", err)
	}
	if err := Set(bg, db, "p1", "city", "London", SourceAI, "cv.pdf"); err != nil {
		t.Fatal(err)
	}
	if err := Set(bg, db, "p1", "city", "Paris", SourceAI, "id.pdf"); err != nil {
		t.Errorf("AI over its own value: %v", err)
	}
	if err := Set(bg, db, "p1", "card_number", "4242", SourceAI, "x"); err == nil {
		t.Error("the AI wrote a card field")
	}
	if err := Set(bg, db, "p1", "city", "Rome", SourceUser, ""); err != nil {
		t.Fatal(err)
	}
	got, _ := Get(bg, db, "p1", false)
	for _, e := range got {
		if e.Key == "city" && (e.Value != "Rome" || e.Source != SourceUser) {
			t.Errorf("a person's edit takes the field back: %+v", e)
		}
	}
}

func TestPhotosAreCopiedIntoTheProfileFolder(t *testing.T) {
	db, root := newDB(t)
	src := filepath.Join(t.TempDir(), "me.PNG")
	if err := os.WriteFile(src, []byte("png-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetPhoto(bg, db, "p1", "portrait_photo", src); err != nil {
		t.Fatal(err)
	}
	p := PhotoPath(bg, db, "p1", "portrait_photo")
	if p != filepath.Join(root, "info", "portrait_photo.png") {
		t.Fatalf("path %q", p)
	}
	if b, _ := os.ReadFile(p); string(b) != "png-bytes" {
		t.Errorf("content %q", b)
	}
	jpg := filepath.Join(t.TempDir(), "me.jpg")
	_ = os.WriteFile(jpg, []byte("jpg"), 0o600)
	if err := SetPhoto(bg, db, "p1", "portrait_photo", jpg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("the old photo of another type was left behind")
	}
	if err := SetPhoto(bg, db, "p1", "first_name", jpg); err == nil {
		t.Error("a photo was accepted for a text field")
	}
	txt := filepath.Join(t.TempDir(), "x.txt")
	_ = os.WriteFile(txt, []byte("x"), 0o600)
	if err := SetPhoto(bg, db, "p1", "portrait_photo", txt); err == nil {
		t.Error("a .txt was accepted as a photo")
	}
	if err := Remove(bg, db, "p1", "portrait_photo"); err != nil {
		t.Fatal(err)
	}
	if PhotoPath(bg, db, "p1", "portrait_photo") != "" {
		t.Error("photo still there after removal")
	}
	if err := Set(bg, db, "p1", "portrait_photo", "x", SourceUser, ""); err == nil {
		t.Error("Set accepted a photo field")
	}
}

func TestEmailAndPhoneAreChecked(t *testing.T) {
	db, _ := newDB(t)
	for k, v := range map[string]string{"email": "ada@example.com", "phone": "+49 (170) 123-4567", "emergency_phone": "0170/1234567"} {
		if err := Set(bg, db, "p1", k, v, SourceUser, ""); err != nil {
			t.Errorf("%s=%q: %v", k, v, err)
		}
	}
	for k, v := range map[string]string{"email": "not-an-email", "email_secondary": "a@b", "phone": "!!", "phone_home": "123", "phone_work": "call me"} {
		if err := Set(bg, db, "p1", k, v, SourceUser, ""); err == nil {
			t.Errorf("%s=%q was accepted", k, v)
		}
	}
}
