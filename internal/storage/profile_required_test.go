package storage_test

import (
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/testdb"
)

// Every profile-scoped storage call refuses an empty profile with
// ErrNoProfile; nothing falls back to "default".
func TestEmptyProfileIsRefused(t *testing.T) {
	db := testdb.Open(t)
	calls := map[string]func() error{
		"UpsertPerson": func() error {
			return db.UpsertPerson(&storage.Person{PlatformUsername: "a", Platform: "x"})
		},
		"BatchUpsertPeople": func() error {
			return db.BatchUpsertPeople([]*storage.Person{{PlatformUsername: "a", Platform: "x"}})
		},
		"GetPersonByUsername": func() error { _, err := db.GetPersonByUsername("a", "x", ""); return err },
		"UpsertPersonMessage": func() error {
			return db.UpsertPersonMessage(&storage.PersonMessage{PersonID: "p", Source: "x"}, "")
		},
		"ListPersonMessages":            func() error { _, err := db.ListPersonMessages("p", "", "", 1, 0); return err },
		"ListPersonMessagesByStatus":    func() error { _, err := db.ListPersonMessagesByStatus("", "draft"); return err },
		"ListAllPersonMessages":         func() error { _, err := db.ListAllPersonMessages("", "", 1, 0); return err },
		"ListAllPersonMessagesFiltered": func() error { _, err := db.ListAllPersonMessagesFiltered("", "", false, 1, 0); return err },
		"AddPersonStatusUpdate":         func() error { _, err := db.AddPersonStatusUpdate("p", "", "t"); return err },
		"GetLatestPersonStatusUpdate":   func() error { _, err := db.GetLatestPersonStatusUpdate("p", ""); return err },
		"ListPersonStatusUpdates":       func() error { _, err := db.ListPersonStatusUpdates("p", "", 1); return err },
		"MarkPersonMessagesRead":        func() error { _, err := db.MarkPersonMessagesRead("", "p", nil); return err },
		"MarkPersonMessageUnread":       func() error { return db.MarkPersonMessageUnread("", "m") },
		"CountUnreadPersonMessages":     func() error { _, err := db.CountUnreadPersonMessages(""); return err },
		"InsertPersonLink": func() error {
			_, err := db.InsertPersonLink(&storage.PersonLink{PersonA: "a", PersonB: "b", Relation: storage.PersonLinkSame, Status: storage.PersonLinkSuggested})
			return err
		},
		"ListPersonLinks":     func() error { _, err := db.ListPersonLinks("", ""); return err },
		"SetPersonLinkStatus": func() error { return db.SetPersonLinkStatus("", "id", storage.PersonLinkConfirmed) },
		"PersonLinksFor":      func() error { _, err := db.PersonLinksFor("", "p"); return err },
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, storage.ErrNoProfile) {
			t.Errorf("%s with no profile = %v, want ErrNoProfile", name, err)
		}
	}
}
