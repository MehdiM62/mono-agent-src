package aitask

import (
	"fmt"
	"regexp"
	"strings"
)

// RedactedValue replaces what looks like a secret in a result.
const RedactedValue = "[redacted]"

// The same rules as internal/applications/assets.go (looksSecret), which is
// unexported there: a label that names a password, token, one-time code, card
// number, cvv or ssn, or a value shaped like a key or token.
var (
	secretLabel = regexp.MustCompile(`(?i)pass(word|code|phrase)?\b|secret|token|api[ _-]?key|otp|2fa|cvv|cvc|card[ _-]?number|\bssn\b|private[ _-]?key`)
	secretValue = regexp.MustCompile(`^(sk-|ghp_|gho_|xox[bap]-|AKIA|eyJ|Bearer\s)|-----BEGIN|^[A-Za-z0-9+/_=-]{40,}$`)
)

func looksSecret(label, value string) bool {
	return secretLabel.MatchString(label) || secretValue.MatchString(strings.TrimSpace(value))
}

// labelKeys name the key of a {label, value} pair in a field report.
var labelKeys = []string{"label", "name", "field"}

// RedactResult returns v (decoded JSON) with secrets removed, before the
// result is stored: every value under a key that names a secret, the value of a
// {label, value} pair whose label names one (or whose value looks like one),
// and every string shaped like a key or token. Nothing in the result is
// trusted to have left them out.
func RedactResult(v any) any {
	switch t := v.(type) {
	case map[string]any:
		label := ""
		for _, k := range labelKeys {
			if s, ok := t[k].(string); ok {
				label = s
				break
			}
		}
		out := make(map[string]any, len(t))
		for k, e := range t {
			switch {
			case secretLabel.MatchString(k) && e != nil:
				out[k] = RedactedValue
			case k == "value" && label != "" && looksSecret(label, scalarText(e)):
				out[k] = RedactedValue
			default:
				out[k] = RedactResult(e)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = RedactResult(e)
		}
		return out
	case string:
		if secretValue.MatchString(strings.TrimSpace(t)) {
			return RedactedValue
		}
	}
	return v
}

func scalarText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil, map[string]any, []any:
		return ""
	}
	return fmt.Sprint(v)
}
