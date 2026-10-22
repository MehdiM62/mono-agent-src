package applyfill

import "regexp"

// The same field vetting as the extension's form filler
// (internal/extension/form_fill.go: formSecretName, formSkipTypes): a field
// that holds a password, a card number, a security or one-time code is never
// filled. The agent is told so; a result that lists one anyway is dropped
// before it is recorded, so the fill record never shows such a value.
var secretLabel = regexp.MustCompile(`(?i)pass(word|code)|\bpin\b|cvv|cvc|security.?code|card.?(number|no)|\botp\b|one.?time|verification.?code|2fa|^cc-|current-password|new-password`)

// Field is one field the agent says it filled.
type Field struct {
	Label     string `json:"label"`
	Value     string `json:"value"`
	Source    string `json:"source"`
	Generated bool   `json:"generated"`
}

// Vet drops the fields that must never have been filled and returns the rest
// with the number dropped.
func Vet(fields []Field) (kept []Field, dropped int) {
	for _, f := range fields {
		if secretLabel.MatchString(f.Label) {
			dropped++
			continue
		}
		kept = append(kept, f)
	}
	return kept, dropped
}
