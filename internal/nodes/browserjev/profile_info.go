package browserjev

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/monoes/mono-agent/internal/profileinfo"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// withProfileInfo adds the run's profile's Info page to values (config
// profile_info): each filled-in field becomes a value named by its key
// (first_name, email, …), so the goal can refer to the person's own details
// without spelling them out. Photos and the card are not values; ID and bank
// numbers are, but as secrets (scrubbed everywhere, a stricter match). A value
// the node's own `values` already names is kept as configured.
func withProfileInfo(ctx context.Context, values []namedValue) ([]namedValue, error) {
	db := vault.DBFromContext(ctx)
	if db == nil {
		return nil, fmt.Errorf("%w: browser.jev profile_info needs a profile database", workflow.ErrInvalidConfig)
	}
	profile, err := vault.ProfileIDFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("browser.jev profile_info: ran without a profile: %w", err)
	}
	entries, err := profileinfo.Get(ctx, db, profile, true)
	if err != nil {
		return nil, fmt.Errorf("browser.jev profile_info: %w", err)
	}
	typ, err := profileinfo.TypeOf(ctx, db, profile)
	if err != nil {
		return nil, fmt.Errorf("browser.jev profile_info: %w", err)
	}
	have := map[string]bool{}
	for _, v := range values {
		have[strings.ToLower(v.Name)] = true
	}
	out := values
	for _, e := range entries {
		fd := profileinfo.Lookup(e.Key)
		v := strings.TrimSpace(e.Value)
		if fd == nil || v == "" || fd.Card || fd.Kind == profileinfo.Photo || fd.Key == profileinfo.TypeKey || !fd.InScope(typ) ||
			have[strings.ToLower(fd.Key)] || strings.EqualFold(fd.Key, noneValue) {
			continue
		}
		// A short secret cannot be kept out of page text (parseValues).
		if fd.Sensitive && utf8.RuneCountInString(v) < scrubMinLen {
			continue
		}
		out = append(out, namedValue{Name: fd.Key, Value: v, Secret: fd.Sensitive})
	}
	sortValues(out)
	return out, nil
}
