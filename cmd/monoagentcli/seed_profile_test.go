package main

import (
	"testing"

	"github.com/monoes/mono-agent/internal/storage"
)

// seedProfile adds profile id (named id) to the database at dbPath.
func seedProfile(t *testing.T, dbPath, id string) {
	t.Helper()
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.DB.Exec(`INSERT INTO profiles (id, name) VALUES (?, ?)`, id, id); err != nil {
		t.Fatal(err)
	}
}
