package hilsend

import (
	"net"
	"strings"
	"unicode"
	"unicode/utf8"
)

// addressTokens finds what in s addresses someone: email addresses (a@b,
// no dot needed), links (any scheme with a host) and tel:, sip: and sms:
// URIs, @handles and Matrix ids (@user:server, #room:server,
// !room:server), #channels, Slack-style ids (C0…, c0…), phone numbers and
// long numbers (numberTokens), IP literals, bare domains with a known TLD
// (evil.com), and UUIDs.
func addressTokens(s string) []string {
	out := numberTokens(s, nil)
	for _, t := range strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",;\"'<>()[]{}|\\`", r)
	}) {
		if t = strings.TrimRight(t, ".:!?"); t == "" {
			continue
		}
		switch at := strings.LastIndexByte(t, '@'); {
		case at > 0 && at < len(t)-1:
			out = append(out, t)
		case strings.Contains(t, "://") && !strings.HasSuffix(t, "://") && !strings.HasPrefix(t, "://"):
			out = append(out, t)
		case uriScheme(t):
			out = append(out, t)
		case (t[0] == '@' || t[0] == '#') && len(t) >= 3 && handle(t[1:]):
			out = append(out, t)
		case t[0] == '!' && strings.Contains(t, ":") && handle(t[1:]):
			out = append(out, t)
		case channelID(t), ipv6(t), bareDomain(t):
			out = append(out, t)
		default:
			if u := uuidIn(t); u != "" {
				out = append(out, u)
			}
		}
	}
	return out
}

// numberTokens appends to out the numbers in s that may be someone's: a
// phone number, 7 to 15 digits with or without a leading +, spaces,
// dashes, dots and parentheses between them (as "+" and the digits, or the
// digits); a long number of up to 20 digits and nothing between (an
// Asana gid, a Discord id); and an IPv4 literal. A date (2026-10-19,
// 19.10.2026), a decimal number (3.14159265) and digits after a letter, a
// dot, -, _, / or $ (v1234567, 08:30:00.123456789, ID-1234567) are none.
func numberTokens(s string, out []string) []string {
	for i := 0; i < len(s); i++ {
		plus := s[i] == '+' && i+1 < len(s) && isDigit(s[i+1])
		if !plus {
			if !isDigit(s[i]) {
				continue
			}
			if r, _ := utf8.DecodeLastRuneInString(s[:i]); i > 0 && !numberStart(r) {
				continue
			}
			if n := dateAt(s[i:]); n > 0 {
				i += n - 1
				continue
			}
		}
		start, j := i, i
		if plus {
			j++
		}
		var digits []byte
		seps, onlyDots := 0, true
		for j < len(s) && len(digits) <= 20 {
			if isDigit(s[j]) {
				digits = append(digits, s[j])
				j++
				continue
			}
			k := j // up to 3 separators, then a digit
			for n := 0; k < len(s) && n < 3; n++ {
				r, size := utf8.DecodeRuneInString(s[k:])
				if !numberSep(r) {
					break
				}
				k += size
			}
			if k == j || k == len(s) || !isDigit(s[k]) {
				break
			}
			seps++
			onlyDots = onlyDots && s[j:k] == "."
			j = k
		}
		n := len(digits)
		switch after, _ := utf8.DecodeRuneInString(s[j:]); {
		case plus:
			if n >= 7 && n <= 15 {
				out = append(out, "+"+string(digits))
			}
		case j < len(s) && (unicode.IsLetter(after) || unicode.IsDigit(after) || after == '_' || after == '@'):
		case seps == 3 && onlyDots && net.ParseIP(s[start:j]) != nil:
			out = append(out, s[start:j])
		case seps == 1 && onlyDots: // a decimal number
		case n >= 7 && (n <= 15 || seps == 0 && n <= 20):
			out = append(out, string(digits))
		}
		i = j - 1
	}
	return out
}

// numberStart reports a rune after which a number may start.
func numberStart(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("._+-/$", r)
}

// numberSep reports a rune between a phone number's digits: a space, a
// dash, a dot or a parenthesis.
func numberSep(r rune) bool {
	return unicode.IsSpace(r) || unicode.Is(unicode.Pd, r) || r == '.' || r == '(' || r == ')'
}

// dateAt is the length of the date at s's start (2026-10-19, 19.10.2026,
// 10-19-2026), 0 when there is none.
func dateAt(s string) int {
	if len(s) < 10 || len(s) > 10 && isDigit(s[10]) {
		return 0
	}
	digits := func(i, j int) bool {
		for ; i < j; i++ {
			if !isDigit(s[i]) {
				return false
			}
		}
		return true
	}
	switch {
	case digits(0, 4) && s[4] == '-' && digits(5, 7) && s[7] == '-' && digits(8, 10):
	case digits(0, 2) && (s[2] == '.' || s[2] == '-') && digits(3, 5) && s[5] == s[2] && digits(6, 10):
	default:
		return 0
	}
	return 10
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

// knownTLDs are the top-level domains a bare domain (evil.com) is read
// with: common generic ones and country ones that are neither an English
// word (to, me, it, name, info) nor a file extension (sh, py, md, pl, so),
// so that neither prose nor a path such as user.name is taken for one.
var knownTLDs = map[string]bool{}

func init() {
	for _, d := range strings.Fields(`com net org edu gov mil biz xyz icu io ai co tv fm ly gg app dev eu uk de
		fr es nl ch se dk fi ie pt cz sk hu ro bg gr ru ua cn jp kr tw hk sg th vn ph au nz ca mx br ar cl pe za
		ng ke eg tr il ir pk bd lk`) {
		knownTLDs[d] = true
	}
}

// bareDomain reports a domain without a scheme (evil.com, mail.evil.co.uk,
// evil.io/pay): labels of letters, digits and dashes, ending in a known
// TLD.
func bareDomain(t string) bool {
	host := t
	if i := strings.IndexAny(host, "/?#:"); i >= 0 {
		host = host[:i]
	}
	dot := strings.LastIndexByte(host, '.')
	if dot <= 0 || !knownTLDs[strings.ToLower(host[dot+1:])] {
		return false
	}
	for _, l := range strings.Split(host, ".") {
		if l == "" || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for i := 0; i < len(l); i++ {
			if b := l[i]; !isAlnum(b) && b != '-' {
				return false
			}
		}
	}
	return true
}

// uuidIn is the first UUID (8-4-4-4-12 hex digits) in t, "" when there is
// none.
func uuidIn(t string) string {
	for i := 0; i+36 <= len(t); i++ {
		if (i == 0 || !isAlnum(t[i-1])) && uuidAt(t[i:]) && (i+36 == len(t) || !isAlnum(t[i+36])) {
			return t[i : i+36]
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
