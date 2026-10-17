package browserctl

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// Host canonical forms (plan C9 item 2). A site rule and a page are
// compared in the form the browser itself resolves: lowercase, IDNA
// (fullwidth dots and Unicode labels become xn--), a numeric IPv4 host as
// dotted decimal, trailing dots trimmed, the default port dropped. So
// "https://BANK.example./", "https://bank\uff0eexample" (a fullwidth dot) and
// "http://bank.example:8443" all reach a block on bank.example.

// asciiHost is host as the browser puts it in an origin: lowercase, mapped
// by IDNA's Lookup profile, a numeric IPv4 form read as the browser reads
// it; a trailing dot is kept. An IPv6 literal is written one way, and an
// IPv4-mapped one as the IPv4 address it reaches. ok is false for a name the browser could read
// differently from us (a non-ASCII name IDNA rejects, a number that is no
// IPv4 address): it matches no rule and an agent is refused it.
func asciiHost(host string) (string, bool) {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return "", false
	}
	if strings.Contains(h, ":") { // an IPv6 literal, in one spelling: [0::1] is [::1]
		ip := net.ParseIP(h)
		switch {
		case ip == nil:
			return h, false
		case ip.To4() != nil: // IPv4-mapped: [::ffff:a9fe:a9fe] is 169.254.169.254
			return ip.To4().String(), true
		}
		return ip.String(), true
	}
	if a, err := idna.Lookup.ToASCII(h); err == nil {
		h = a
	} else if !plainASCII(h) {
		return h, false
	}
	trimmed := strings.TrimRight(h, ".")
	ip, numeric, ok := ipv4Literal(trimmed)
	switch {
	case !numeric:
		return h, true
	case !ok:
		return h, false
	}
	return ip + h[len(trimmed):], true
}

// canonHost is asciiHost with its trailing dots trimmed: the form rules
// are matched in.
func canonHost(host string) (string, bool) {
	h, ok := asciiHost(host)
	return strings.TrimRight(h, "."), ok
}

// canonOrigin is u's origin in matching form: scheme://canonHost[:port],
// the scheme's default port dropped.
func canonOrigin(u *url.URL) (string, bool) {
	host, ok := canonHost(u.Hostname())
	return joinOrigin(strings.ToLower(u.Scheme), host, u.Port()), ok
}

func joinOrigin(scheme, host, port string) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" && port != defaultPort(scheme) {
		host += ":" + port
	}
	return scheme + "://" + host
}

// BrowserOrigin is rawURL's origin exactly as Chrome serializes it (the
// extension's `new URL(u).origin`), for what the extension compares with
// a tab's origin (expectOrigin, a read's origin filter): the host
// lowercase and in its xn-- form, a numeric IPv4 host dotted, a trailing
// dot kept, an IPv6 literal compressed and bracketed, an IPv4-mapped one
// in hex ("http://[::ffff:7f00:1]"), the default port dropped. Origin, not
// this, is the key policy and rules match on.
func BrowserOrigin(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return shortURL(rawURL)
	}
	host := u.Hostname()
	if ip, err := netip.ParseAddr(host); err == nil && ip.Is6() && ip.Zone() == "" {
		host = whatwgIPv6(ip)
	} else {
		host, _ = asciiHost(host)
	}
	return joinOrigin(strings.ToLower(u.Scheme), host, u.Port())
}

// whatwgIPv6 writes ip as the URL standard does: eight lowercase hex
// pieces with the first longest run of two or more zero pieces as "::",
// an embedded IPv4 address in hex too.
func whatwgIPv6(ip netip.Addr) string {
	b := ip.As16()
	var pieces [8]uint16
	for i := range pieces {
		pieces[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}
	start, length := -1, 1
	for i := 0; i < 8; {
		if pieces[i] != 0 {
			i++
			continue
		}
		j := i
		for j < 8 && pieces[j] == 0 {
			j++
		}
		if j-i > length {
			start, length = i, j-i
		}
		i = j
	}
	var sb strings.Builder
	for i := 0; i < 8; i++ {
		if i == start {
			sb.WriteString("::")
			i += length - 1
			continue
		}
		if i > 0 && i != start+length { // "::" already ends with the separator
			sb.WriteByte(':')
		}
		sb.WriteString(strconv.FormatUint(uint64(pieces[i]), 16))
	}
	return sb.String()
}

// UnicodeHost is host for a person to read: its xn-- labels decoded. It is
// host itself when there is nothing to decode.
func UnicodeHost(host string) string {
	a, ok := asciiHost(host)
	if !ok {
		return host
	}
	if u, err := idna.Lookup.ToUnicode(a); err == nil {
		return u
	}
	return a
}

// idnForms is host's xn-- form and the Unicode form it reads as, when host
// is an internationalized name (the two differ); "", "" otherwise. A HIL
// card shows both, so a look-alike name can't pass as the real one.
func idnForms(host string) (ascii, unicode string) {
	a, ok := canonHost(host)
	if !ok {
		return "", ""
	}
	if u := UnicodeHost(a); u != a {
		return a, u
	}
	return "", ""
}

// itemHost is rawURL's host[:port] for a HIL item's name; an
// internationalized name reads "bücher.example (xn--bcher-kva.example)".
func itemHost(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return "a page"
	}
	a, uni := idnForms(u.Hostname())
	if a == "" {
		return u.Host
	}
	if p := u.Port(); p != "" {
		uni += ":" + p
	}
	return uni + " (" + a + ")"
}

// plainASCII: only the characters of an ordinary host name, which the
// browser keeps as they are (IDNA's stricter rules reject some, such as
// "_" or "--" in the third and fourth place).
func plainASCII(h string) bool {
	for _, r := range h {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// ipv4Literal reads h as the browser does when its last label is a number
// (WHATWG URL): "0x7f.1", "2130706433" and "127.0.0.1" are all 127.0.0.1.
// numeric is false when h is a name; ok is false for a number that is no
// IPv4 address (the browser refuses such a URL).
func ipv4Literal(h string) (ip string, numeric, ok bool) {
	parts := strings.Split(h, ".")
	if !numericLabel(parts[len(parts)-1]) {
		return "", false, true
	}
	if len(parts) > 4 {
		return "", true, false
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		n, ok := ipv4Number(p)
		if !ok || (i < len(parts)-1 && n > 255) {
			return "", true, false
		}
		nums[i] = n
	}
	last := nums[len(nums)-1]
	if last >= uint64(1)<<(8*(5-len(nums))) {
		return "", true, false
	}
	v := last
	for i, n := range nums[:len(nums)-1] {
		v += n << (8 * (3 - i))
	}
	return fmt.Sprintf("%d.%d.%d.%d", byte(v>>24), byte(v>>16), byte(v>>8), byte(v)), true, true
}

func numericLabel(p string) bool {
	if rest, ok := strings.CutPrefix(p, "0x"); ok {
		_, err := strconv.ParseUint("0"+rest, 16, 64)
		return err == nil
	}
	if p == "" {
		return false
	}
	for _, r := range p {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func ipv4Number(p string) (uint64, bool) {
	base := 10
	switch {
	case p == "":
		return 0, false
	case strings.HasPrefix(p, "0x"):
		p, base = "0"+p[2:], 16
	case len(p) > 1 && p[0] == '0':
		p, base = p[1:], 8
	}
	n, err := strconv.ParseUint(p, base, 64)
	return n, err == nil
}
