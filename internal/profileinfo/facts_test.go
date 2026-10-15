package profileinfo

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

func TestWithDocumentNamesUsesTheUploadName(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE vault_documents (path TEXT, filename TEXT, profile_id TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range [][]any{
		{"/home/x/.monoagent/profiles/p1/.monoagent/vault/documents/doc-2513.pdf", "CV Ada Lovelace (2).pdf", "p1"},
		{"/elsewhere/doc-2513.pdf", "Someone else's.pdf", "p2"},
		{"/p/doc_1.pdf", "underscore.pdf", "p1"},
	} {
		if _, err := db.Exec(`INSERT INTO vault_documents VALUES (?,?,?)`, r...); err != nil {
			t.Fatal(err)
		}
	}
	got := withDocumentNames(context.Background(), db, "p1", []Fact{
		{Source: "/any/where/doc-2513.pdf", Text: "a"},
		{Source: "doc-2513.pdf", Text: "b"},
		{Source: "/x/unknown.pdf", Text: "c"},
		{Source: "/x/docX1.pdf", Text: "d"}, // _ must not act as a wildcard
	})
	want := []string{"CV Ada Lovelace (2).pdf", "CV Ada Lovelace (2).pdf", "/x/unknown.pdf", "/x/docX1.pdf"}
	for i, f := range got {
		if f.Source != want[i] {
			t.Errorf("fact %d: %q, want %q", i, f.Source, want[i])
		}
	}
}
