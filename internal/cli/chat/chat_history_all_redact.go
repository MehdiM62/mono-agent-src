package chat

import (
	"regexp"
	"strings"

	"github.com/monoes/mono-agent/internal/aitask"
)

// A secret label followed by its value: "password: hunter2", "token=abc".
var titleSecretPair = regexp.MustCompile(`(?i)\b(pass(?:word|code|phrase)?|secret|token|api[ _-]?key|otp|cvv|private[ _-]?key)\b(\s*[:=]\s*|\s+is\s+)\S+`)

// redactTitleText removes what looks like a secret from text that becomes a
// title: a labelled value, then every word aitask.RedactResult takes for a key
// or token (the same shapes a task result is cleaned of).
func redactTitleText(s string) string {
	s = titleSecretPair.ReplaceAllString(s, "$1: "+aitask.RedactedValue)
	words := strings.Fields(s)
	for i, w := range words {
		if r, ok := aitask.RedactResult(w).(string); ok {
			words[i] = r
		}
	}
	return strings.Join(words, " ")
}
