package recording

import (
	"bytes"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// Privacy re-check (spec §8.2, security review H6/L1). The recorder already
// withholds these values; ingest checks again because the extension is not
// the only thing that can write to this socket, and a value that reached
// disk is a value that reaches the analyzer's prompt.

// sensitiveNameRe matches field names and labels that announce a secret.
// Deliberately broad: masking a harmless field costs a manual input at
// review time, missing a secret costs the secret.
var sensitiveNameRe = regexp.MustCompile(`(?i)pass|pwd|token|secret|(^|[^a-z])(pin|cvv|cvc|csc|otp|ssn|2fa|mfa)([^a-z]|$)`)

// SensitiveTarget reports whether a fingerprint names a field whose value
// must never be stored: the recorder flagged it, it is a password or hidden
// input, the page marks it with a card / password autocomplete token, or
// its name or label says it holds a secret.
func SensitiveTarget(fp *Fingerprint) bool {
	if fp == nil {
		return false
	}
	if fp.Sensitive {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(fp.InputType)) {
	case "password", "hidden":
		return true
	}
	if sensitiveAutocomplete(fp.Autocomplete) {
		return true
	}
	for _, s := range []string{fp.Name, fp.ID, fp.Label, fp.AriaName, fp.Placeholder, fp.TestID} {
		if s != "" && sensitiveNameRe.MatchString(s) {
			return true
		}
	}
	return false
}

func sensitiveAutocomplete(ac string) bool {
	for _, tok := range strings.Fields(strings.ToLower(ac)) {
		if strings.HasPrefix(tok, "cc-") || tok == "current-password" || tok == "new-password" || tok == "one-time-code" {
			return true
		}
	}
	return false
}

// Length caps for free text an event or start frame carries.
const (
	MaxGoalRunes  = 500
	MaxTitleRunes = 300
	MaxNoteRunes  = 500
)

// Sanitize makes an event safe to store: URLs sanitised, the value of a
// sensitive field dropped and marked Masked, free text capped.
func Sanitize(ev *Event) {
	if ev == nil {
		return
	}
	sanitizeEventURLs(ev)
	ev.Note = clipRunes(ev.Note, MaxNoteRunes)
	if !SensitiveTarget(ev.Target) {
		return
	}
	ev.Target.Sensitive = true
	if ev.Value != "" || ev.Type == EvType || ev.Type == EvSelect {
		ev.Value = ""
		ev.Masked = true
	}
}

// CleanGoal is a recording's goal as it is kept and handed to the analysis:
// one line of at most MaxGoalRunes runes, without what shows nothing on
// screen but a model still reads (see invisible).
func CleanGoal(s string) string {
	s = strings.Map(func(r rune) rune {
		if invisible(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	return clipRunes(strings.Join(strings.Fields(s), " "), MaxGoalRunes)
}

// invisible reports a character that shows nothing: a control, the Unicode
// tag block (invisible "ASCII smuggling"), a variation selector, a format
// character (bidi controls, zero-width ones, the byte order mark), a
// private-use character, or a code point with no character assigned.
func invisible(r rune) bool {
	switch {
	case r >= 0xE0000 && r <= 0xE007F:
		return true
	case unicode.IsControl(r), unicode.Is(unicode.Cf, r), unicode.Is(unicode.Co, r), unicode.Is(unicode.Variation_Selector, r):
		return true
	}
	return !unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S, unicode.Z)
}

// clipRunes caps s at n runes.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ScrubHTML rewrites a DOM snippet so it carries no secrets:
//
//   - value attributes of sensitive inputs are removed (every input's, and
//     every textarea's text, when allValues is set — the snippet belongs to
//     an event on a sensitive field);
//   - data-* attributes are removed (apps park tokens and ids in them);
//   - inline event handlers (on*) are removed: they are page code, and can
//     carry a token (onclick="fetch(…, {Authorization: 'Bearer …'})");
//   - URLs in href/src/action/formaction are sanitised (SanitizeURL), and
//     javascript:/vbscript: ones removed;
//   - <script> elements are dropped.
//
// It uses an HTML tokenizer, not a pattern: attribute order, quoting and
// case cannot hide a value from it.
func ScrubHTML(src string, allValues bool) string {
	z := html.NewTokenizer(strings.NewReader(src))
	var out bytes.Buffer
	skip := "" // element whose content is being dropped
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			// io.EOF or a malformed tail: either way, what was
			// rewritten so far is all that is kept.
			return out.String()
		}
		tok := z.Token()
		if skip != "" {
			if tt == html.EndTagToken && tok.Data == skip {
				skip = ""
				if tok.Data != "script" {
					out.WriteString(tok.String())
				}
			}
			continue
		}
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			if tok.Data == "script" {
				if tt == html.StartTagToken {
					skip = "script"
				}
				continue
			}
			sensitive := allValues || sensitiveElement(tok)
			tok.Attr = scrubAttrs(tok, sensitive)
			out.WriteString(tok.String())
			if tok.Data == "textarea" && sensitive && tt == html.StartTagToken {
				skip = "textarea"
			}
		default:
			out.WriteString(tok.String())
		}
	}
}

// sensitiveElement reports whether a form control's value must go.
func sensitiveElement(tok html.Token) bool {
	if tok.Data != "input" && tok.Data != "textarea" {
		return false
	}
	for _, a := range tok.Attr {
		v := strings.ToLower(a.Val)
		switch strings.ToLower(a.Key) {
		case "type":
			if t := strings.TrimSpace(v); t == "password" || t == "hidden" {
				return true
			}
		case "autocomplete":
			if sensitiveAutocomplete(v) {
				return true
			}
		case "style":
			if strings.Contains(v, "-webkit-text-security") {
				return true
			}
		case "name", "id", "aria-label", "placeholder":
			if sensitiveNameRe.MatchString(a.Val) {
				return true
			}
		}
	}
	return false
}

// scriptURL reports a javascript:/vbscript: URL. Browsers skip tabs,
// newlines and other controls inside a scheme, so they are skipped here too.
func scriptURL(v string) bool {
	var b strings.Builder
	for _, r := range v {
		if r > ' ' {
			b.WriteRune(r)
		}
	}
	s := strings.ToLower(b.String())
	return strings.HasPrefix(s, "javascript:") || strings.HasPrefix(s, "vbscript:")
}

func scrubAttrs(tok html.Token, sensitive bool) []html.Attribute {
	kept := tok.Attr[:0]
	for _, a := range tok.Attr {
		key := strings.ToLower(a.Key)
		if a.Namespace != "" {
			key = strings.ToLower(a.Namespace) + ":" + key
		}
		switch {
		case strings.HasPrefix(key, "data-"), strings.HasPrefix(key, "on"):
			continue
		case key == "value" && sensitive:
			continue
		case key == "href" || key == "src" || key == "action" || key == "formaction" || key == "xlink:href":
			if scriptURL(a.Val) {
				continue
			}
			a.Val = SanitizeURL(a.Val)
		}
		kept = append(kept, a)
	}
	return kept
}
