package profile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

type infoDoc struct {
	Entries []struct {
		Key, Value, Source string
		Masked             bool
	} `json:"entries"`
}

func TestProfileInfoFieldsSetGetRm(t *testing.T) {
	keyring.MockInit()
	dbPath := newProfileDocsCLITestDB(t)

	var cat struct {
		Groups []struct{ ID, Label string }
		Fields []struct{ Key, Kind, Group string }
	}
	mustProfileJSON(t, dbPath, &cat, "info", "fields")
	if len(cat.Fields) < 100 || len(cat.Groups) < 10 {
		t.Fatalf("catalog: %d fields, %d groups", len(cat.Fields), len(cat.Groups))
	}

	for _, kv := range [][2]string{{"first_name", "Ada"}, {"title", "ms."}, {"passport_number", "X1234567"}} {
		if _, err := runProfileJSON(t, dbPath, "info", "set", kv[0], kv[1]); err != nil {
			t.Fatalf("set %s: %v", kv[0], err)
		}
	}
	var got infoDoc
	mustProfileJSON(t, dbPath, &got, "info", "get")
	if len(got.Entries) != 3 || got.Entries[0].Value != "Ms." || !got.Entries[2].Masked || got.Entries[2].Value != "•••• 4567" {
		t.Errorf("get: %+v", got.Entries)
	}
	mustProfileJSON(t, dbPath, &got, "info", "get", "--reveal")
	if got.Entries[2].Value != "X1234567" {
		t.Errorf("reveal: %+v", got.Entries[2])
	}

	if _, err := runProfileJSON(t, dbPath, "info", "set", "title", "Emperor"); err == nil || exitCodeOf(t, err) != 3 {
		t.Errorf("a bad option: %v", err)
	}
	if _, err := runProfileJSON(t, dbPath, "info", "set", "nope", "x"); err == nil || exitCodeOf(t, err) != 3 {
		t.Errorf("an unknown field: %v", err)
	}
	if _, err := runProfileJSON(t, dbPath, "info", "rm", "first_name"); err != nil {
		t.Fatal(err)
	}
	mustProfileJSON(t, dbPath, &got, "info", "get")
	if len(got.Entries) != 2 {
		t.Errorf("after rm: %+v", got.Entries)
	}
}

func TestProfileInfoPhoto(t *testing.T) {
	keyring.MockInit()
	dbPath := newProfileDocsCLITestDB(t)
	src := filepath.Join(t.TempDir(), "me.jpg")
	if err := os.WriteFile(src, []byte("jpg"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runProfileJSON(t, dbPath, "info", "photo", "portrait_photo", src); err != nil {
		t.Fatal(err)
	}
	var p struct{ Path string }
	mustProfileJSON(t, dbPath, &p, "info", "photo-path", "portrait_photo")
	if b, err := os.ReadFile(p.Path); err != nil || string(b) != "jpg" {
		t.Errorf("photo at %q: %q, %v", p.Path, b, err)
	}
	if _, err := runProfileJSON(t, dbPath, "info", "photo", "portrait_photo", filepath.Join(t.TempDir(), "none.jpg")); err == nil || exitCodeOf(t, err) != 2 {
		t.Errorf("a missing file: %v", err)
	}
	if _, err := runProfileJSON(t, dbPath, "info", "photo", "first_name", src); err == nil || exitCodeOf(t, err) != 3 {
		t.Errorf("a photo for a text field: %v", err)
	}
}
