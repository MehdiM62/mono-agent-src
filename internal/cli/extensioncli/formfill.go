package extensioncli

import (
	"context"
	"os"
	"path/filepath"
	"strings"

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

// Info implements extension.FormFiller: the profile's Info page values, minus
// photos and the card (a card is filled only by its own button, never here).
func (f formFiller) Info(ctx context.Context, profile string) ([]extension.InfoValue, error) {
	db, err := capturecli.OpenProfileDB(f.path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	entries, err := profileinfo.Get(ctx, db.DB, profile, true)
	if err != nil {
		return nil, err
	}
	var out []extension.InfoValue
	for _, e := range entries {
		fd := profileinfo.Lookup(e.Key)
		if fd == nil || fd.Card || fd.Kind == profileinfo.Photo || e.Value == "" {
			continue
		}
		out = append(out, extension.InfoValue{
			Key: fd.Key, Label: fd.Label, Kind: string(fd.Kind), Autocomplete: fd.Autocomplete,
			Sensitive: fd.Sensitive, Value: e.Value,
		})
	}
	return out, nil
}

var _ extension.FormAssets = formFiller{}

// Card implements extension.FormAssets: the card the person typed on the Info
// page, if any. Nothing is read or invented from documents.
func (f formFiller) Card(ctx context.Context, profile string) (extension.CardDetails, bool, error) {
	db, err := capturecli.OpenProfileDB(f.path)
	if err != nil {
		return extension.CardDetails{}, false, err
	}
	defer db.Close()
	entries, err := profileinfo.Get(ctx, db.DB, profile, true)
	if err != nil {
		return extension.CardDetails{}, false, err
	}
	var c extension.CardDetails
	for _, e := range entries {
		switch e.Key {
		case "card_holder":
			c.Holder = e.Value
		case "card_number":
			c.Number = e.Value
		case "card_expiry":
			c.Expiry = e.Value
		case "card_cvv":
			c.CVV = e.Value
		}
	}
	return c, c.Number != "", nil
}

var photoMimes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp",
	".gif": "image/gif", ".pdf": "application/pdf",
}

// Photo implements extension.FormAssets: a stored photo or scan.
func (f formFiller) Photo(ctx context.Context, profile, key string) (extension.PhotoFile, bool, error) {
	db, err := capturecli.OpenProfileDB(f.path)
	if err != nil {
		return extension.PhotoFile{}, false, err
	}
	defer db.Close()
	path := profileinfo.PhotoPath(ctx, db.DB, profile, key)
	if path == "" {
		return extension.PhotoFile{}, false, nil
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 10<<20 {
		return extension.PhotoFile{}, false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return extension.PhotoFile{}, false, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	return extension.PhotoFile{Name: key + ext, Mime: photoMimes[ext], Data: data}, true, nil
}

// Complete implements extension.FormFiller.
func (f formFiller) Complete(ctx context.Context, runtime, model, prompt string) (string, error) {
	if runtime == "" {
		runtime = f.runtime
	}
	a, err := f.run(ctx, capturesummary.Target{Runtime: runtime, Model: model}, prompt)
	return a.Text, err
}
