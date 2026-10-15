package extensioncli

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/monoes/mono-agent/internal/capturesummary"
	capturecli "github.com/monoes/mono-agent/internal/cli/capture"
	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/monomind"
)

// The form-filling half of the extension bridge: form.fill reads the named
// profile's own documents and captures (never the global brain) and asks a
// tool-less model turn which field each fact belongs to
// (internal/extension/form_fill.go). Like the task sink it opens the database
// per request and never creates one.
type formFiller struct {
	path    string
	runtime string
	run     capturesummary.RunFunc
}

func extensionFormFiller(path, runtime string) extension.FormFiller {
	return formFiller{path: path, runtime: runtime, run: capturesummary.ExecRunner(0)}
}

// Facts implements extension.FormFiller.
//
// The knowledge search matches words that are literally in a document, so
// "address" or "phone number" finds nothing in a CV that just lists them in a
// table. What does work is a document's own name ("CV Ada Lovelace" finds the
// block with her contact details), so the names of the profile's indexed
// documents are searched first, then the form's own queries.
func (f formFiller) Facts(ctx context.Context, profile string, queries []string) ([]extension.FormFact, error) {
	db, err := capturecli.OpenProfileDB(f.path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	all := append(docNameQueries(ctx, db.DB, profile), queries...)
	type found struct {
		facts []extension.FormFact
		err   error
	}
	results := make([]found, len(all))
	var wg sync.WaitGroup
	for i, q := range all {
		wg.Add(1)
		go func(i int, q string) {
			defer wg.Done()
			res, err := monomind.SearchKnowledge(ctx, db.DB, profile, q)
			results[i].err = err
			for _, r := range res {
				results[i].facts = append(results[i].facts, extension.FormFact{Source: r.Path, Text: r.Excerpt})
			}
		}(i, q)
	}
	wg.Wait()
	var facts []extension.FormFact
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
	if fails == len(all) && first != nil {
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

// Complete implements extension.FormFiller.
func (f formFiller) Complete(ctx context.Context, runtime, model, prompt string) (string, error) {
	if runtime == "" {
		runtime = f.runtime
	}
	a, err := f.run(ctx, capturesummary.Target{Runtime: runtime, Model: model}, prompt)
	return a.Text, err
}
