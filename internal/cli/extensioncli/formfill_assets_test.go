package extensioncli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/profileinfo"
	"github.com/monoes/mono-agent/internal/storage"
)

// infoDB is a migrated database with profile "p1" whose folder is a temp dir.
func infoDB(t *testing.T) (string, *storage.Database) {
	t.Helper()
	keyring.MockInit()
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := storage.NewDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.DB.Exec(`INSERT INTO profiles (id, name, root_dir) VALUES ('p1', 'one', ?)`, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	return path, db
}

func TestFormFillerReadsInfoCardAndPhotosFromTheInfoPage(t *testing.T) {
	path, db := infoDB(t)
	ctx := context.Background()
	for k, v := range map[string]string{
		"first_name": "Ada", "passport_number": "X1234567",
		"card_holder": "Ada Lovelace", "card_number": "4242 4242 4242 4242", "card_expiry": "01/27", "card_cvv": "123",
	} {
		if err := profileinfo.Set(ctx, db.DB, "p1", k, v, profileinfo.SourceUser, ""); err != nil {
			t.Fatal(err)
		}
	}
	src := filepath.Join(t.TempDir(), "me.png")
	if err := os.WriteFile(src, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := profileinfo.SetPhoto(ctx, db.DB, "p1", "portrait_photo", src); err != nil {
		t.Fatal(err)
	}

	f := formFiller{path: path}
	info, err := f.Info(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, v := range info {
		got[v.Key] = v.Sensitive
		if strings.HasPrefix(v.Key, "card_") || v.Key == "portrait_photo" {
			t.Errorf("Info offers %s to Fill form", v.Key)
		}
	}
	if sens, ok := got["passport_number"]; !ok || !sens || len(info) != 2 {
		t.Errorf("info: %+v", info)
	}

	card, ok, err := f.Card(ctx, "p1")
	if err != nil || !ok || card.Number != "4242 4242 4242 4242" || card.Holder != "Ada Lovelace" || card.Expiry != "01/27" || card.CVV != "123" {
		t.Errorf("card: %+v ok=%v err=%v", card, ok, err)
	}
	photo, ok, err := f.Photo(ctx, "p1", "portrait_photo")
	if err != nil || !ok || string(photo.Data) != "png" || photo.Mime != "image/png" || photo.Name != "portrait_photo.png" {
		t.Errorf("photo: %+v ok=%v err=%v", photo, ok, err)
	}
	if _, ok, _ := f.Photo(ctx, "p1", "id_front_photo"); ok {
		t.Error("a photo that was never stored")
	}
	if _, ok, _ := f.Card(ctx, "other"); ok {
		t.Error("another profile's card")
	}
}

func TestFormFillerOffersOnlyTheFieldsOfTheProfilesType(t *testing.T) {
	path, db := infoDB(t)
	ctx := context.Background()
	set := func(k, v string) {
		t.Helper()
		if err := profileinfo.Set(ctx, db.DB, "p1", k, v, profileinfo.SourceUser, ""); err != nil {
			t.Fatal(err)
		}
	}
	set("first_name", "Ada")
	set("company_name", "Acme GmbH")
	set("iban", "DE89370400440532013000")
	keys := func() map[string]bool {
		info, err := formFiller{path: path}.Info(ctx, "p1")
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]bool{}
		for _, v := range info {
			m[v.Key] = true
		}
		return m
	}
	if got := keys(); !got["first_name"] || !got["iban"] || got["company_name"] || got["profile_type"] {
		t.Errorf("person: %v", got)
	}
	set("profile_type", "Business")
	if got := keys(); got["first_name"] || !got["company_name"] || !got["iban"] || got["profile_type"] {
		t.Errorf("business: %v", got)
	}
}
