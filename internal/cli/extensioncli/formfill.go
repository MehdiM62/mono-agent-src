package extensioncli

import (
	"context"

	"github.com/monoes/mono-agent/internal/capturesummary"
	capturecli "github.com/monoes/mono-agent/internal/cli/capture"
	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/profileinfo"
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

// Facts implements extension.FormFiller: the profile's own documents and
// captures, found by profileinfo.GatherFacts (which also searches by document
// name, because the knowledge search matches only literal words).
func (f formFiller) Facts(ctx context.Context, profile string, queries []string) ([]extension.FormFact, error) {
	db, err := capturecli.OpenProfileDB(f.path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	found, err := profileinfo.GatherFacts(ctx, db.DB, profile, queries)
	if err != nil {
		return nil, err
	}
	out := make([]extension.FormFact, len(found))
	for i, x := range found {
		out[i] = extension.FormFact{Source: x.Source, Text: x.Text}
	}
	return out, nil
}

// Complete implements extension.FormFiller.
func (f formFiller) Complete(ctx context.Context, runtime, model, prompt string) (string, error) {
	if runtime == "" {
		runtime = f.runtime
	}
	a, err := f.run(ctx, capturesummary.Target{Runtime: runtime, Model: model}, prompt)
	return a.Text, err
}
