package aitask

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// RedactedValue replaces what looks like a secret in a result.
const RedactedValue = "[redacted]"

var (
	// placeholderValue is a value that names a secret instead of holding one: a
	// double-brace secret, vault or expression reference, or the REPLACE_ME a
	// draft leaves. The WHOLE value must match: text around a placeholder, or a
	// brace pair holding anything else, is not one. Never redacted: it is what a
	// valid workflow holds in its credential fields.
	placeholderValue = regexp.MustCompile(`^(?:\{\{\s*(?:secret|vault|\$)[A-Za-z0-9_.: -]{0,100}\}\}|REPLACE_ME)$`)

	// The shapes of a secret anywhere inside a string. RE2: linear in the
	// text, no backtracking, so a hostile result cannot make this slow.
	embeddedToken = regexp.MustCompile(`\b(?:sk[-_](?:live|test)[-_][A-Za-z0-9_-]{8,}|sk-[A-Za-z0-9_-]{16,}|gh[pousr]_[A-Za-z0-9]{16,}|xox[a-z]-[A-Za-z0-9-]{8,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{30,}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}(?:\.[A-Za-z0-9_-]*)?)`)
	privateKey    = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY`)
	bearerToken   = regexp.MustCompile(`(?i)\bBearer\s+(\S{16,})`)
	basicAuth     = regexp.MustCompile(`(?i)(\bAuthorization\s*[:=]\s*["']?\s*)Basic\s+(\S{8,})`)
	basicValue    = regexp.MustCompile(`(?i)^\s*Basic\s+\S{8,}`)
	urlUserinfo   = regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://[^\s:/@]+:)([^\s@/]+)(@)`)
	queryParam    = regexp.MustCompile(`(?i)([?&](?:(?:access|refresh|id)_` + `tok` + `en=|` + `tok` + `en=)|[?&](?:api[_-]?key|client_secret|secret|password|passwd)=)([^&\s"'#]{8,})`)
	// A secret-named label with its value in prose ("password: Hunter2!"). The
	// value must hold something other than lowercase letters, so a sentence
	// such as "password: required" stays.
	labelledValue = regexp.MustCompile(`(?i)\b(pass(?:word|wd|code|phrase)|secret|api[ _-]?key|` + `tok` + `en)(\s*[:=]\s*)(\S{8,})`)
)

// secretWords and secretPairs name a secret when they are WORDS of a label or
// key (split on separators and camelCase), so "footprint" is not "otp" and
// "tokenizer" is not a token.
var (
	secretWords = map[string]bool{"password": true, "passwd": true, "pass": true, "passcode": true, "passphrase": true, "passport": true,
		"secret": true, "tok" + "en": true, "apikey": true, "otp": true, "2fa": true, "cvv": true, "cvc": true, "ssn": true, "iban": true, "privatekey": true}
	secretPairs = map[string]bool{"api key": true, "private key": true, "card number": true, "card no": true, "social security": true,
		"access key": true, "secret key": true}
	// A key that also holds one of these words describes a secret (its name, its
	// limit, its type) and is not one.
	metadataWords = map[string]bool{"name": true, "id": true, "ref": true, "enabled": true, "type": true, "limit": true, "count": true, "max": true,
		"min": true, "usage": true, "ttl": true, "expires": true, "expiry": true, "length": true, "size": true, "budget": true, "label": true,
		"status": true, "field": true, "hint": true, "used": true, "total": true}
)

// labelWords splits a label or key into lowercase words: separators break it,
// and so does a lower-to-upper change (apiKey, passportNumber).
func labelWords(s string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	var prev rune
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if unicode.IsUpper(r) && unicode.IsLower(prev) {
				flush()
			}
			cur = append(cur, r)
		} else {
			flush()
		}
		prev = r
	}
	flush()
	return words
}

// isSecretLabel says whether a label or JSON key names a secret.
func isSecretLabel(s string) bool {
	words := labelWords(s)
	hit := false
	for i, w := range words {
		if secretWords[w] || (i+1 < len(words) && secretPairs[w+" "+words[i+1]]) {
			hit = true
		}
	}
	if !hit {
		return false
	}
	for _, w := range words {
		if metadataWords[w] {
			return false
		}
	}
	return true
}

func looksSecret(label, value string) bool {
	if placeholderValue.MatchString(value) {
		return false
	}
	return isSecretLabel(label) || redactString(strings.TrimSpace(value)) != strings.TrimSpace(value)
}

// hasMoreThanLowercase is true when s holds a digit, an upper-case letter or a symbol.
func hasMoreThanLowercase(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z') {
			return true
		}
	}
	return false
}

// redactString removes what looks like a secret from s: every key- or
// token-shaped word inside it, a private key block, a bearer or basic
// credential, the password of a URL, the value of a credential query
// parameter and the value of a secret-named label in prose. A placeholder
// stays; it is accepted only as the whole string (or the whole value).
func redactString(s string) string {
	if placeholderValue.MatchString(s) {
		return s
	}
	if privateKey.MatchString(s) {
		return RedactedValue
	}
	s = embeddedToken.ReplaceAllString(s, RedactedValue)
	s = bearerToken.ReplaceAllStringFunc(s, func(m string) string {
		f := strings.Fields(m)
		if placeholderValue.MatchString(f[len(f)-1]) {
			return m
		}
		return f[0] + " " + RedactedValue
	})
	s = basicAuth.ReplaceAllString(s, "${1}Basic "+RedactedValue)
	s = urlUserinfo.ReplaceAllStringFunc(s, func(m string) string {
		g := urlUserinfo.FindStringSubmatch(m)
		if placeholderValue.MatchString(g[2]) {
			return m
		}
		return g[1] + RedactedValue + g[3]
	})
	s = queryParam.ReplaceAllStringFunc(s, func(m string) string {
		g := queryParam.FindStringSubmatch(m)
		if placeholderValue.MatchString(g[2]) {
			return m
		}
		return g[1] + RedactedValue
	})
	var out strings.Builder
	last := 0
	for _, loc := range labelledValue.FindAllStringSubmatchIndex(s, -1) {
		// "secret:" inside an open double-brace reference is its prefix, not a label.
		if before := s[:loc[0]]; strings.LastIndex(before, "{{") > strings.LastIndex(before, "}}") {
			continue
		}
		v := strings.Trim(s[loc[6]:loc[7]], `"',;`)
		if placeholderValue.MatchString(v) || !hasMoreThanLowercase(v) || strings.HasPrefix(v, RedactedValue) {
			continue
		}
		out.WriteString(s[last:loc[6]])
		out.WriteString(RedactedValue)
		last = loc[7]
	}
	out.WriteString(s[last:])
	return out.String()
}

// labelKeys name the key of a {label, value} pair in a field report.
var labelKeys = []string{"label", "name", "field"}

// RedactResult returns v (decoded JSON) with secrets removed, before the
// result is stored: the string value under a key that names a secret, the
// value of a {label, value} pair whose label names one (or whose value looks
// like one), a secret-shaped key name, and every secret inside a string.
// Nothing in the result is trusted to have left them out.
//
// It runs after the schema check, so it only ever swaps a STRING for another
// string: numbers, booleans, nulls, lists and objects keep their type (a count
// under a key such as a token limit stays a number), and the replacement is
// short and never empty, so type, length and enum checks hold. Placeholders and
// metadata keys (max_tokens, token_type, secret_name) are left alone.
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
			name := k
			if r := redactString(k); r != k {
				name = r
				for i := 2; ; i++ {
					_, inInput := t[name]
					_, inOutput := out[name]
					if !inInput && !inOutput {
						break
					}
					name = fmt.Sprintf("%s %d", r, i)
				}
			}
			switch s := e.(type) {
			case string:
				switch {
				case isSecretLabel(k) && s != "" && !placeholderValue.MatchString(s):
					out[name] = RedactedValue
				case k == "value" && label != "" && looksSecret(label, s):
					out[name] = RedactedValue
				case strings.EqualFold(k, "authorization") && basicValue.MatchString(s):
					out[name] = "Basic " + RedactedValue
				default:
					out[name] = redactString(s)
				}
			case float64:
				// A number typed as a number under a secret LABEL ({"label":"Card number",
				// "value":4111...}) is still the secret; a count under a key stays a number.
				if k == "value" && label != "" && isSecretLabel(label) {
					out[name] = RedactedValue
				} else {
					out[name] = e
				}
			default:
				out[name] = RedactResult(e)
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
