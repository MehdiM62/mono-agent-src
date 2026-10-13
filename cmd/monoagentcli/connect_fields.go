package main

import (
	"sort"
	"strings"

	"github.com/monoes/mono-agent/internal/connections"
)

// parseOAuthFieldFlags turns --field key=value flags into a map, rejecting a
// key that is not one of allowed (the error names the allowed keys).
func parseOAuthFieldFlags(platform string, flags []string, allowed []connections.CredentialField) (map[string]string, error) {
	keys := make(map[string]bool, len(allowed))
	names := make([]string, 0, len(allowed))
	for _, f := range allowed {
		keys[f.Key] = true
		names = append(names, f.Key)
	}
	sort.Strings(names)
	values := map[string]string{}
	for _, kv := range flags {
		k, v, ok := strings.Cut(kv, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, errInvalidInput("--field %q: use key=value", kv)
		}
		if !keys[k] {
			if len(names) == 0 {
				return nil, errInvalidInput("--field %s: %s's OAuth takes no fields", k, platform)
			}
			return nil, errInvalidInput("--field %s: %s's OAuth takes %s", k, platform, strings.Join(names, ", "))
		}
		values[k] = v
	}
	return values, nil
}

// missingRequired names the required fields values has no value for.
func missingRequired(fields []connections.CredentialField, values map[string]string) []string {
	var out []string
	for _, f := range fields {
		if f.Required && strings.TrimSpace(values[f.Key]) == "" {
			out = append(out, f.Key)
		}
	}
	return out
}

// secretValues are the values of fields' secret fields, to scrub from
// error messages.
func secretValues(fields []connections.CredentialField, values map[string]string) []string {
	var out []string
	for _, f := range fields {
		if f.Secret && values[f.Key] != "" {
			out = append(out, values[f.Key])
		}
	}
	return out
}

// hasMethod reports whether p offers method m.
func hasMethod(p connections.PlatformDef, m connections.AuthMethod) bool {
	for _, pm := range p.Methods {
		if pm == m {
			return true
		}
	}
	return false
}
