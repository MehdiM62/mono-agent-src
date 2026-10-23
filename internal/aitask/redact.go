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
	secretLabel = regexp.MustCompile(`(?i)pass(word|code|phrase|port)?\b|secret|token|api[ _-]?key|otp|2fa|cvv|cvc|card[ _-]?(number|no\b)|\bssn\b|private[ _-]?key|\biban\b|social[ _-]?security`)
	secretValue = regexp.MustCompile(`^(sk-|ghp_|gho_|xox[bap]-|AKIA|eyJ|Bearer\s)|-----BEGIN|^[A-Za-z0-9+/_=-]{40,}$`)

	// placeholderValue is a value that names a secret instead of holding one: a
	// double-brace template or secret reference (the double-brace secret form
	// of a workflow) or the REPLACE_ME a draft leaves. Never redacted: it is
	// what a valid workflow holds in its credential fields.
	placeholderValue = regexp.MustCompile(`^\s*(?:\{\{[^{}]{1,200}\}\}|REPLACE_ME)\s*$`)

	// The shapes of a secret anywhere inside a string. RE2: linear in the
	// text, no backtracking, so a hostile result cannot make this slow.
	embeddedToken     = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{16,}|gh[pousr]_[A-Za-z0-9]{16,}|xox[a-z]-[A-Za-z0-9-]{8,}|AKIA[0-9A-Z]{16}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}(?:\.[A-Za-z0-9_-]*)?)`)
	bearerPlaceholder = regexp.MustCompile(`(?i)^\s*Bearer\s+\{\{`)
	bearerToken       = regexp.MustCompile(`(?i)\bBearer\s+(\S{16,})`)
	queryToken        = regexp.MustCompile(`(?i)([?&](?:(?:access|refresh|id)_)?token=|[?&](?:api[_-]?key|client_secret|secret|password|passwd)=)([^&\s"'#]{8,})`)
)

func looksSecret(label, value string) bool {
	if placeholderValue.MatchString(value) {
		return false
	}
	return secretLabel.MatchString(label) || secretValue.MatchString(strings.TrimSpace(value))
}

// redactString removes what looks like a secret from s: the whole string when
// it is one, else every key- or token-shaped word inside it, a bearer token
// and the value of a token-like query parameter. A placeholder stays.
func redactString(s string) string {
	if placeholderValue.MatchString(s) {
		return s
	}
	if secretValue.MatchString(strings.TrimSpace(s)) && !bearerPlaceholder.MatchString(s) {
		return RedactedValue
	}
	s = embeddedToken.ReplaceAllString(s, RedactedValue)
	s = bearerToken.ReplaceAllStringFunc(s, func(m string) string {
		f := strings.Fields(m)
		if strings.HasPrefix(f[len(f)-1], "{{") {
			return m
		}
		return f[0] + " " + RedactedValue
	})
	return queryToken.ReplaceAllStringFunc(s, func(m string) string {
		i := strings.IndexByte(m, '=')
		if strings.HasPrefix(m[i+1:], "{{") {
			return m
		}
		return m[:i+1] + RedactedValue
	})
}

// keepsSecretNamedValue says whether a value under a secret-named key is not
// a secret: a placeholder, a boolean, or a count under a key such as
// max_tokens (usage numbers, not credentials).
func keepsSecretNamedValue(key string, e any) bool {
	switch t := e.(type) {
	case string:
		return placeholderValue.MatchString(t)
	case bool:
		return true
	case float64:
		return strings.HasSuffix(strings.ToLower(key), "tokens")
	}
	return false
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
			case secretLabel.MatchString(k) && e != nil && keepsSecretNamedValue(k, e):
				out[k] = e
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
		return redactString(t)
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
