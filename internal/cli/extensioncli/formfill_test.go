package extensioncli

import (
	"context"
	"database/sql"
	"reflect"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDocNameQuery(t *testing.T) {
	for in, want := range map[string]string{
		"CV Ada Lovelace (2).pdf":      "Ada Lovelace",
		"passport_ada-lovelace.PDF":    "passport ada lovelace",
		"cover_letter-1538481644.html": "cover letter",
		"12345.pdf":                    "",
		"Müller Zoë final copy.docx":   "Müller Zoë",
	} {
		if got := docNameQuery(in); got != want {
			t.Errorf("docNameQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDocNameQueriesUsesOnlyThisProfilesIndexedDocuments(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE vault_documents (seq INTEGER, filename TEXT, profile_id TEXT, indexed INTEGER)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range [][]any{
		{1, "CV Ada Lovelace.pdf", "p1", 1},
		{2, "CV Ada Lovelace (2).pdf", "p1", 1}, // same words: one query
		{3, "notes.txt", "p1", 0},               // not indexed
		{4, "passport.pdf", "other", 1},         // another profile
		{5, "tax return 2025.pdf", "p1", 1},
	} {
		if _, err := db.Exec(`INSERT INTO vault_documents VALUES (?,?,?,?)`, r...); err != nil {
			t.Fatal(err)
		}
	}
	got := docNameQueries(context.Background(), db, "p1")
	if want := []string{"tax return", "Ada Lovelace"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
