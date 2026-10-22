package chat

import (
	"regexp"
	"strings"

	"github.com/monoes/mono-agent/internal/aitask"
)

// Credential shapes that span several words. They are replaced on the whole
// text, before it is split: a per-word check can't see "Bearer <token>" as one
// expression, and a short token has no shape of its own.
var (
	// scheme://user:pass@host and scheme://token@host.
	titleURLUserinfo = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^\s/@]+@`)
	// Authorization: Bearer x, Authorization=Basic x, Authorization: x.
	titleAuthHeader = regexp.MustCompile(`(?i)\bauthorization\b\s*[:=]\s*(?:(?:bearer|basic|token|digest)\s+)?\S+`)
	// Bearer <anything non-space>.
	titleBearer = regexp.MustCompile(`(?i)\b(bearer)\s+\S+`)
	// Basic <base64>: at least 8 base64 characters with a digit, a symbol or
	// mixed case, so "basic understanding" stays.
	titleBasic = regexp.MustCompile(`(?i)\b(basic)\s+([A-Za-z0-9+/]{8,}={0,2})`)
	// A secret-named key with its value: password, passwd, token, secret,
	// api_key, apikey, cookie, … as "key=value", "key: value" or a quoted value.
	titleSecretPair = regexp.MustCompile(`(?i)\b[\w.-]*(?:pass(?:word|wd|code|phrase)?|secret|token|api[ _-]?key|apikey|cookie|otp|cvv|private[ _-]?key|access[ _-]?key)[\w.-]*\s*[:=]\s*(?:"[^"]*"|'[^']*'|\S+)`)
	// "the password is x": only the bare label, so "tokens is" is prose.
	titleSecretIs = regexp.MustCompile(`(?i)\b(?:pass(?:word|wd|code|phrase)?|secret|token|api[ _-]?key|apikey|private[ _-]?key)\s+is\s+\S+`)
)

// redactTitleText removes what looks like a secret from text that becomes a
// title: whole credential expressions first, then every word
// aitask.RedactResult takes for a key or token (the same shapes a task result
// is cleaned of).
func redactTitleText(s string) string {
	s = titleURLUserinfo.ReplaceAllString(s, "${1}"+aitask.RedactedValue+"@")
	s = titleAuthHeader.ReplaceAllString(s, "Authorization: "+aitask.RedactedValue)
	s = titleBearer.ReplaceAllString(s, "${1} "+aitask.RedactedValue)
	s = titleBasic.ReplaceAllStringFunc(s, func(m string) string {
		parts := strings.Fields(m)
		if !looksLikeBasicCredential(parts[len(parts)-1]) {
			return m
		}
		return parts[0] + " " + aitask.RedactedValue
	})
	s = titleSecretPair.ReplaceAllStringFunc(s, func(m string) string {
		if i := strings.IndexAny(m, ":= \t"); i > 0 {
			return m[:i] + ": " + aitask.RedactedValue
		}
		return aitask.RedactedValue
	})
	s = titleSecretIs.ReplaceAllStringFunc(s, func(m string) string { return strings.Fields(m)[0] + ": " + aitask.RedactedValue })
	words := strings.Fields(s)
	for i, w := range words {
		if r, ok := aitask.RedactResult(w).(string); ok {
			words[i] = r
		}
	}
	return strings.Join(words, " ")
}

func looksLikeBasicCredential(tok string) bool {
	return strings.ContainsAny(tok, "0123456789+/=") || (tok != strings.ToLower(tok) && tok != strings.ToUpper(tok))
}
