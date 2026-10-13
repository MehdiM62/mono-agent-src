package httpnodes

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"net/http"
	"strings"
)

// digestChallenge is a parsed "WWW-Authenticate: Digest ..." header.
type digestChallenge struct {
	realm, nonce, opaque, algorithm, qop string
}

// parseDigestChallenge reads the Digest challenge from a 401 response; ok
// is false when the server did not ask for Digest auth.
func parseDigestChallenge(h http.Header) (digestChallenge, bool) {
	for _, v := range h.Values("Www-Authenticate") {
		scheme, rest, _ := strings.Cut(strings.TrimSpace(v), " ")
		if !strings.EqualFold(scheme, "Digest") {
			continue
		}
		var c digestChallenge
		for _, part := range splitAuthParams(rest) {
			k, val, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			val = strings.Trim(strings.TrimSpace(val), `"`)
			switch strings.ToLower(strings.TrimSpace(k)) {
			case "realm":
				c.realm = val
			case "nonce":
				c.nonce = val
			case "opaque":
				c.opaque = val
			case "algorithm":
				c.algorithm = val
			case "qop":
				c.qop = val
			}
		}
		return c, c.nonce != ""
	}
	return digestChallenge{}, false
}

// splitAuthParams splits comma-separated auth params, keeping commas that
// sit inside quoted values (qop="auth,auth-int").
func splitAuthParams(s string) []string {
	var parts []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case r == ',' && !inQuote:
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

// digestAuthorization builds the Authorization header answering c (RFC
// 7616: MD5 or SHA-256, with or without -sess, qop "auth" or none).
func digestAuthorization(c digestChallenge, user, pw, method, uri string) (string, error) {
	alg := strings.ToUpper(c.algorithm)
	var newHash func() hash.Hash
	switch alg {
	case "", "MD5", "MD5-SESS":
		newHash = md5.New
	case "SHA-256", "SHA-256-SESS":
		newHash = sha256.New
	default:
		return "", fmt.Errorf("digest auth: unsupported algorithm %q", c.algorithm)
	}
	h := func(s string) string {
		x := newHash()
		x.Write([]byte(s))
		return hex.EncodeToString(x.Sum(nil))
	}
	cnonceRaw := make([]byte, 8)
	if _, err := rand.Read(cnonceRaw); err != nil {
		return "", fmt.Errorf("digest auth: %w", err)
	}
	cnonce := hex.EncodeToString(cnonceRaw)
	nc := "00000001"

	ha1 := h(user + ":" + c.realm + ":" + pw)
	if strings.HasSuffix(alg, "-SESS") {
		ha1 = h(ha1 + ":" + c.nonce + ":" + cnonce)
	}
	ha2 := h(method + ":" + uri)

	qop := ""
	for _, q := range strings.Split(c.qop, ",") {
		if strings.TrimSpace(q) == "auth" {
			qop = "auth"
		}
	}
	var response string
	if qop != "" {
		response = h(ha1 + ":" + c.nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		response = h(ha1 + ":" + c.nonce + ":" + ha2)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `Digest username=%q, realm=%q, nonce=%q, uri=%q, response=%q`, user, c.realm, c.nonce, uri, response)
	if c.algorithm != "" {
		fmt.Fprintf(&b, `, algorithm=%s`, c.algorithm)
	}
	if c.opaque != "" {
		fmt.Fprintf(&b, `, opaque=%q`, c.opaque)
	}
	if qop != "" {
		fmt.Fprintf(&b, `, qop=%s, nc=%s, cnonce=%q`, qop, nc, cnonce)
	}
	return b.String(), nil
}
