package applyfill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/monoes/mono-agent/internal/aitask"
)

// Input is what a fill task is given about one application.
type Input struct {
	ApplicationID string
	Title         string
	URL           string
	// PostingText is the application's captured posting text ("" when none was captured).
	PostingText string
	// Facts is the profile facts (Facts).
	Facts string
	// Documents are the files registered to this application, and no others.
	Documents []string
}

// Goal is the task's goal text. It carries the application's title and URL,
// which are the user's own records; the posting text only goes in a context file.
func (in Input) Goal() string {
	return fmt.Sprintf("Fill in the application form for %q at %s. Do not submit it.", oneLine(in.Title), oneLine(in.URL))
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// WriteSources writes the posting and the facts into dir (a folder of the
// caller's, made for this task) and returns the --context sources: posting=,
// facts= and one document= for each registered document.
func (in Input) WriteSources(dir string) ([]aitask.ContextSource, error) {
	var out []aitask.ContextSource
	add := func(class, name, text string) error {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			return err
		}
		out = append(out, aitask.ContextSource{Class: class, Path: p})
		return nil
	}
	if err := add(aitask.SourcePosting, "posting.txt", "URL: "+oneLine(in.URL)+"\n\n"+in.PostingText); err != nil {
		return nil, err
	}
	if err := add(aitask.SourceFacts, "facts.txt", in.Facts); err != nil {
		return nil, err
	}
	for _, d := range in.Documents {
		out = append(out, aitask.ContextSource{Class: aitask.SourceDocument, Path: d})
	}
	return out, nil
}
