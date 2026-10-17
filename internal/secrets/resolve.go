package secrets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
)

const secretRefPrefix = "@secret:"

// ErrResolveRefused is wrapped by the refusal to resolve a reference under
// a context RefuseResolving marked.
var ErrResolveRefused = errors.New("secret references are not read from the vault here")

// refusedError is that refusal: the reference and why, which already says
// what ErrResolveRefused would.
type refusedError struct{ msg string }

func (e *refusedError) Error() string { return e.msg }
func (e *refusedError) Unwrap() error { return ErrResolveRefused }

// ErrDataReference is wrapped by the refusal of a @secret reference that a
// step's config holds only once its expressions were evaluated.
var ErrDataReference = errors.New("a @secret reference that came from the run's data or an expression is not read from the vault")

type refuseResolvingKey struct{}

// RefuseResolving marks ctx: no @secret reference resolves under it, nor a
// {{secret:}} lookup or anything else that reads the vault through Resolve.
// why says what runs under ctx, for the refusal, which names the reference
// and never a value. A run the user hasn't approved is one (the workflow
// package marks it).
func RefuseResolving(ctx context.Context, why string) context.Context {
	return context.WithValue(ctx, refuseResolvingKey{}, why)
}

// ResolveRefusal is why references aren't resolved under ctx; "" when they
// are.
func ResolveRefusal(ctx context.Context) string {
	why, _ := ctx.Value(refuseResolvingKey{}).(string)
	return why
}

// Resolve turns a `@secret:<name>` reference into a decrypted value stored
// in the vault under profileID. Strings that do not carry the `@secret:`
// prefix are returned unchanged. Returns an error if the reference is
// present but no matching entry is found, and also if the entry holds
// multiple fields with none of them keyed `secret` — see DecryptFields for
// the field-picking rules.
func Resolve(ctx context.Context, db *sql.DB, profileID, ref string) (string, error) {
	if !strings.HasPrefix(ref, secretRefPrefix) {
		return ref, nil
	}
	if why := ResolveRefusal(ctx); why != "" {
		return ref, &refusedError{ref + " is not read from the vault: " + why}
	}
	name := strings.TrimPrefix(ref, secretRefPrefix)

	entries, err := List(ctx, db, profileID)
	if err != nil {
		return ref, fmt.Errorf("secrets.Resolve: %w", err)
	}
	var id string
	for _, e := range entries {
		if e.Name == name {
			id = e.ID
			break
		}
	}
	if id == "" {
		return ref, fmt.Errorf("secrets.Resolve: entry %q not found", name)
	}

	fields, _, err := DecryptFields(ctx, db, profileID, id)
	if err != nil {
		return ref, fmt.Errorf("secrets.Resolve: %w", err)
	}
	if v, ok := fields["secret"]; ok {
		return v, nil
	}
	if len(fields) == 1 {
		for _, v := range fields {
			return v, nil
		}
	}
	return ref, fmt.Errorf("secrets.Resolve: entry %q holds multiple fields; a bare reference needs exactly one field, or one keyed \"secret\"", name)
}

// ResolveConfig walks a config map and replaces any string value that starts
// with "@secret:" with its decrypted value from the vault. Mirrors
// vault.ResolveConfig's behavior: a missing/not-found secret is non-fatal —
// the original "@secret:" string is left in place and a warning is logged to
// stderr, rather than failing the whole config resolution. This is a
// deliberate difference from Resolve/DecryptEntry's own not-found behavior
// (which errors), because at this integration point matching the
// established @img- convention is what's consistent with the surrounding
// code (see internal/workflow/execution.go, right after its
// vault.ResolveConfig call). A reference ctx refuses (RefuseResolving) is
// the one error it returns: the step must not run with it.
//
// stored is the same config as the workflow holds it, before its expressions
// were evaluated into config. A reference is the workflow's own only where
// stored holds it as it is, in a value with no expression: "@secret:" in any
// value an expression produced (the run's input, a webhook body, an earlier
// step's output, or text the expression built) is refused at any depth of
// config, whatever node would read it, and the step doesn't run.
func ResolveConfig(ctx context.Context, db *sql.DB, profileID string, stored, config map[string]interface{}) error {
	if db == nil {
		return nil
	}
	if path, ok := dataReference(stored, config, ""); ok {
		return fmt.Errorf("%q: %w: only one written as it is in the workflow resolves", path, ErrDataReference)
	}
	for k, v := range config {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if !strings.HasPrefix(s, secretRefPrefix) {
			continue
		}
		resolved, err := Resolve(ctx, db, profileID, s)
		if errors.Is(err, ErrResolveRefused) {
			return err
		}
		if err != nil {
			// Non-fatal: leave original ref, emit warning.
			fmt.Fprintf(os.Stderr, "secrets: warning: %v\n", err)
			continue
		}
		config[k] = resolved
	}
	return nil
}

// dataReference is the path in resolved of a string that holds "@secret:"
// where stored, at the same path, isn't that same string free of
// expressions.
func dataReference(stored, resolved interface{}, path string) (string, bool) {
	switch r := resolved.(type) {
	case string:
		if s, ok := stored.(string); ok && s == r && !strings.Contains(s, "{{") {
			return "", false
		}
		return path, strings.Contains(r, secretRefPrefix)
	case map[string]interface{}:
		sm, _ := stored.(map[string]interface{})
		for k, v := range r {
			p := k
			if path != "" {
				p = path + "." + k
			}
			if found, ok := dataReference(sm[k], v, p); ok {
				return found, true
			}
		}
	case []interface{}:
		ss, _ := stored.([]interface{})
		for i, v := range r {
			var sv interface{}
			if i < len(ss) {
				sv = ss[i]
			}
			if found, ok := dataReference(sv, v, fmt.Sprintf("%s[%d]", path, i)); ok {
				return found, true
			}
		}
	}
	return "", false
}
