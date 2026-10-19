package hilsend

import (
	"net"
	"strings"
	"unicode"
)

// addressTokens is what in s addresses someone (scanAddresses).
func addressTokens(s string) []string {
	var out []string
	scanAddresses(s, func(t string) bool {
		out = append(out, t)
		return true
	})
	return out
}

// scanAddresses calls emit with what in s addresses someone, and stops when
// emit returns false (reporting false): phone numbers, long numbers and IP
// literals (numberTokens); then, word by word, email addresses (a@b, no
// dot needed), links (any scheme with a host) and tel:, sip: and sms:
// URIs, @handles and Matrix ids (@user:server, #room:server,
// !room:server), #channels, Slack-style ids (C0…, c0…), IPv6 literals,
// bare domains (bareDomain), and UUIDs.
func scanAddresses(s string, emit func(string) bool) bool {
	if !numberTokens(s, emit) {
		return false
	}
	start := -1
	for i, r := range s {
		if !unicode.IsSpace(r) && !strings.ContainsRune(",;\"'<>()[]{}|\\`", r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 && !addressWord(s[start:i], emit) {
			return false
		}
		start = -1
	}
	return start < 0 || addressWord(s[start:], emit)
}

// addressWord calls emit with word t when it addresses someone, and
// reports emit's answer (true when t doesn't).
func addressWord(t string, emit func(string) bool) bool {
	if t = strings.TrimRight(t, ".:!?"); t == "" {
		return true
	}
	switch at := strings.LastIndexByte(t, '@'); {
	case at > 0 && at < len(t)-1,
		strings.Contains(t, "://") && !strings.HasSuffix(t, "://") && !strings.HasPrefix(t, "://"),
		uriScheme(t),
		(t[0] == '@' || t[0] == '#') && len(t) >= 3 && handle(t[1:]),
		t[0] == '!' && strings.Contains(t, ":") && handle(t[1:]),
		channelID(t), ipv6(t), bareDomain(t):
		return emit(t)
	}
	if u := uuidIn(t); u != "" {
		return emit(u)
	}
	return true
}

// uriScheme reports a tel:, sip:, sips: or sms: URI.
func uriScheme(t string) bool {
	for _, p := range []string{"tel:", "sip:", "sips:", "sms:"} {
		if len(t) > len(p) && strings.EqualFold(t[:len(p)], p) {
			return true
		}
	}
	return false
}

// handle reports a handle's or a channel name's characters, a letter
// among them; a Matrix id's colon (user:server) is one.
func handle(s string) bool {
	letter := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r):
			letter = true
		case unicode.IsDigit(r) || r == '_' || r == '.' || r == '-' || r == ':':
		default:
			return false
		}
	}
	return letter
}

// channelID reports a Slack-style channel, group or user id: C, G, D, U
// or W and 8 to 12 upper-case letters and digits, a digit among them; or
// the same in lower case, a digit after the first letter (c0123abcd9).
func channelID(s string) bool {
	if len(s) < 9 || len(s) > 13 {
		return false
	}
	p, lower := s[0], s[0] >= 'a' && s[0] <= 'z'
	if lower {
		p -= 'a' - 'A'
	}
	if !strings.ContainsRune("CGDUW", rune(p)) || lower && !isDigit(s[1]) {
		return false
	}
	digit := false
	for i := 1; i < len(s); i++ {
		switch b := s[i]; {
		case isDigit(b):
			digit = true
		case !lower && b >= 'A' && b <= 'Z', lower && b >= 'a' && b <= 'z':
		default:
			return false
		}
	}
	return digit
}

// ipv6 reports an IPv6 literal (2001:db8::1).
func ipv6(t string) bool {
	if strings.Count(t, ":") < 2 {
		return false
	}
	hex := false
	for i := 0; i < len(t); i++ {
		switch b := t[i]; {
		case isHex(b):
			hex = true
		case b != ':' && b != '.':
			return false
		}
	}
	return hex && net.ParseIP(t) != nil
}

// fileExts are the endings of a dotted word that make it a file name, not
// a domain (README.md, main.py, console.log).
var fileExts = map[string]bool{}

func init() {
	for _, e := range strings.Fields(`txt md markdown rst js mjs cjs ts tsx jsx json yaml yml toml xml html htm css
		scss less py go rs rb php java kt swift cpp hpp cs sh bash zsh bat exe dll so dylib bin lib jar class wasm
		png jpg jpeg gif svg webp ico bmp tif tiff heic pdf doc docx xls xlsx ppt pptx odt ods csv tsv log ini cfg
		conf env lock sql db sqlite bak tmp tar gz tgz bz2 xz rar mp3 mp4 wav flac ogg avi mkv webm m4a iso dmg pkg
		deb rpm apk ipa map vue svelte pem crt key csr`) {
		fileExts[e] = true
	}
}

// bareDomain reports a host without a scheme: dotted labels of letters,
// digits and dashes ending in a word of letters (evil.com, evil.zip,
// printer.local, an IDN) that isn't a file name's ending or camelCase
// (user.userName); a single label with a port (localhost:8080); or one
// after // (//intranet/hook). A path, query or port may follow.
func bareDomain(t string) bool {
	if strings.HasPrefix(t, "//") {
		rest := t[2:]
		if i := strings.IndexAny(rest, "/?#:"); i >= 0 {
			rest = rest[:i]
		}
		return hostLabel(rest) || bareDomain(t[2:])
	}
	host := t
	if i := strings.IndexAny(host, "/?#:"); i >= 0 {
		host = host[:i]
	}
	dot := strings.LastIndexByte(host, '.')
	if dot < 0 {
		port := strings.TrimPrefix(t[len(host):], ":")
		if i := strings.IndexByte(port, '/'); i >= 0 {
			port = port[:i]
		}
		return len(t) > len(host) && t[len(host)] == ':' && len(port) >= 2 && len(port) <= 5 && digitsValue(port) >= 0 && hostLabel(host)
	}
	tld, camel := host[dot+1:], false
	n, prev := 0, rune(0)
	for _, r := range tld {
		if !unicode.IsLetter(r) {
			return false
		}
		camel = camel || unicode.IsLower(prev) && unicode.IsUpper(r)
		n, prev = n+1, r
	}
	if n < 2 || n > 24 || camel || fileExts[strings.ToLower(tld)] {
		return false
	}
	for _, l := range strings.Split(host[:dot], ".") {
		if !label(l) {
			return false
		}
	}
	return true
}

// label reports a domain label: letters, digits and dashes, not at either
// end.
func label(l string) bool {
	if l == "" || l[0] == '-' || l[len(l)-1] == '-' {
		return false
	}
	for _, r := range l {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' {
			return false
		}
	}
	return true
}

// hostLabel reports a single-label host name, a letter in it.
func hostLabel(l string) bool {
	return label(l) && strings.IndexFunc(l, unicode.IsLetter) >= 0
}

// uuidIn is the first UUID in t (8-4-4-4-12 hex digits, or 32 of them
// without dashes), "" when there is none.
func uuidIn(t string) string {
	for i := 0; i < len(t); i++ {
		if i > 0 && isAlnum(t[i-1]) {
			continue
		}
		if i+36 <= len(t) && uuidAt(t[i:]) && (i+36 == len(t) || !isAlnum(t[i+36])) {
			return t[i : i+36]
		}
		j := i
		for j < len(t) && isHex(t[j]) {
			j++
		}
		if j-i == 32 && (j == len(t) || !isAlnum(t[j])) {
			return t[i:j]
		}
	}
	return ""
}

func uuidAt(s string) bool {
	for k := 0; k < 36; k++ {
		if k == 8 || k == 13 || k == 18 || k == 23 {
			if s[k] != '-' {
				return false
			}
		} else if !isHex(s[k]) {
			return false
		}
	}
	return true
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isHex(b byte) bool { return isDigit(b) || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F' }

func isAlnum(b byte) bool { return isDigit(b) || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }
