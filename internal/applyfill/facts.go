package applyfill

import (
	"context"
	"database/sql"
	"strings"

	"github.com/monoes/mono-agent/internal/profileinfo"
)

// Facts is the profile facts the form fill uses, as text for the task's
// context: the Info page values (internal/cli/extensioncli/formfill.go,
// formFiller.Info) minus what the model is never shown (extension.modelFacts):
// ID and bank numbers (Sensitive), the card, photos and the profile type.
func Facts(ctx context.Context, db *sql.DB, profile string) (string, error) {
	entries, err := profileinfo.Get(ctx, db, profile, true)
	if err != nil {
		return "", err
	}
	typ, err := profileinfo.TypeOf(ctx, db, profile)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, e := range entries {
		fd := profileinfo.Lookup(e.Key)
		if fd == nil || fd.Card || fd.Sensitive || fd.Kind == profileinfo.Photo || e.Value == "" ||
			fd.Key == profileinfo.TypeKey || !fd.InScope(typ) {
			continue
		}
		b.WriteString(fd.Label + ": " + strings.ReplaceAll(strings.TrimSpace(e.Value), "\n", " ") + "\n")
	}
	return b.String(), nil
}
