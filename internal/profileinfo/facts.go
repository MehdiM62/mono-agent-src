package profileinfo

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/monoes/mono-agent/internal/monomind"
)

// Fact is one excerpt found in the profile's documents or captures.
type Fact struct {
	Source string // the file it came from
	Text   string
}

// GatherFacts searches the profile's own knowledge (never the global brain).
//
// The knowledge search matches words that are literally in a document, so
// "address" or "phone number" finds nothing in a CV that just lists them in a
// table. What does work is a document's own name ("CV Ada Lovelace" finds the
// block with her contact details), so the names of the profile's indexed
// documents are searched first, then queries. An error comes back only if
// every search failed.
func GatherFacts(ctx context.Context, db *sql.DB, profile string, queries []string) ([]Fact, error) {
	all := append(docNameQueries(ctx, db, profile), queries...)
	type found struct {
		facts []Fact
		err   error
	}
	results := make([]found, len(all))
	var wg sync.WaitGroup
	for i, q := range all {
		wg.Add(1)
		go func(i int, q string) {
			defer wg.Done()
			res, err := monomind.SearchKnowledge(ctx, db, profile, q)
			results[i].err = err
			for _, r := range res {
				results[i].facts = append(results[i].facts, Fact{Source: r.Path, Text: r.Excerpt})
			}
		}(i, q)
	}
	wg.Wait()
	var facts []Fact
	fails := 0
	var first error
	for _, r := range results {
		if r.err != nil {
			fails++
			if first == nil {
				first = r.err
			}
		}
		facts = append(facts, r.facts...)
	}
	if len(all) > 0 && fails == len(all) && first != nil {
		return nil, first
	}
	return facts, nil
}

// maxDocNameQueries bounds the extra searches: each starts a process.
const maxDocNameQueries = 4

// docNameQueries turns the newest indexed documents' file names into queries.
func docNameQueries(ctx context.Context, db *sql.DB, profile string) []string {
	rows, err := db.QueryContext(ctx, `SELECT filename FROM vault_documents WHERE profile_id = ? AND indexed = 1 ORDER BY seq DESC LIMIT 12`, profile)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	seen := map[string]bool{}
	for rows.Next() && len(out) < maxDocNameQueries {
		var name string
		if rows.Scan(&name) != nil {
			continue
		}
		if q := docNameQuery(name); q != "" && !seen[q] {
			seen[q] = true
			out = append(out, q)
		}
	}
	return out
}

// Words in a file name that say nothing about what the document is about.
var docNameNoise = map[string]bool{
	"copy": true, "final": true, "scan": true, "scanned": true, "version": true, "new": true, "old": true,
	"pdf": true, "doc": true, "docx": true, "txt": true, "the": true, "and": true,
}

// docNameQuery keeps the letter-words of a file name: "CV Ada Lovelace (2).pdf"
// becomes "Ada Lovelace".
func docNameQuery(filename string) string {
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	var words []string
	for _, w := range strings.FieldsFunc(stem, func(r rune) bool { return !unicode.IsLetter(r) }) {
		if len([]rune(w)) >= 3 && !docNameNoise[strings.ToLower(w)] {
			words = append(words, w)
		}
	}
	return strings.Join(words, " ")
}
