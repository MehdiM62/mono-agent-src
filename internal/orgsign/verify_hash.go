package orgsign

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// hmacInput is the string monomind's hmac() signs: JSON.stringify of an
// object literal, so its keys keep the literal's order (kind, v, org, root,
// hash, at), and a missing `v` (undefined) is left out.
func hmacInput(v interface{}, hasV bool, org, root, hash, at string) string {
	var sb strings.Builder
	sb.WriteString(`{"kind":"org-def"`)
	if hasV {
		sb.WriteString(`,"v":`)
		writeValue(&sb, v)
	}
	for _, kv := range [][2]string{{"org", org}, {"root", root}, {"hash", hash}, {"at", at}} {
		sb.WriteString(`,"` + kv[0] + `":`)
		writeString(&sb, kv[1])
	}
	sb.WriteByte('}')
	return sb.String()
}

// evenHexPrefix is what Buffer.from(s, 'hex') decodes: the longest prefix
// of hex digit pairs.
func evenHexPrefix(s string) string {
	n := 0
	for n < len(s) && strings.ContainsRune("0123456789abcdefABCDEF", rune(s[n])) {
		n++
	}
	return s[:n-n%2]
}

// jsString is String(x) for the values a sidecar's `at` can hold.
func jsString(x interface{}) (string, bool) {
	switch t := x.(type) {
	case string:
		return t, true
	case json.Number:
		return stringify(t), true
	case bool:
		return stringify(t), true
	case nil:
		return "", false
	}
	return "", false
}

// Hash is monomind's computeOrgDefHash(raw, root): the sha256 of the
// signed projection, with the digests of any instructions files.
func Hash(root string, raw []byte) (string, error) {
	return hashWith(raw, readDigests(root))
}

// HashPins is Hash that also returns the instructions-file digests it used.
func HashPins(root string, raw []byte) (string, Pins, error) {
	pins := newPins()
	h, err := hashWith(raw, recordingDigests(root, pins))
	return h, pins, err
}

// HashPinned is Hash with the instructions files taken as pins recorded,
// not as they are on disk now; a file not in pins is an error.
func HashPinned(raw []byte, pins Pins) (string, error) {
	return hashWith(raw, pins.pinned())
}

func hashWith(raw []byte, dig digester) (string, error) {
	v, err := parseOrgJSON(raw)
	if err != nil {
		return "", err
	}
	if p := forbiddenKeyPath(v, ""); p != "" {
		return "", fmt.Errorf("holds a forbidden key (%s)", p)
	}
	return hashValue(v, dig)
}

// unpairedSurrogate reports a \u escape of an unpaired surrogate (a high
// \uD800-\uDBFF not followed by an escaped low one, or a lone low
// \uDC00-\uDFFF): Go decodes one to U+FFFD where Node keeps it, so the
// hash could differ. An escaped backslash ("\\ud800" as text) and a valid
// pair (an escaped emoji) decode the same in both and are fine.
func unpairedSurrogate(raw []byte) bool {
	hex4 := func(i int) (int, bool) {
		if i+4 > len(raw) {
			return 0, false
		}
		v, err := strconv.ParseUint(string(raw[i:i+4]), 16, 32)
		return int(v), err == nil
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' || i+1 >= len(raw) {
			continue
		}
		if raw[i+1] != 'u' {
			i++ // \\, \" and the like: skip the escaped character
			continue
		}
		cp, ok := hex4(i + 2)
		if !ok {
			continue
		}
		switch {
		case cp >= 0xD800 && cp <= 0xDBFF:
			if i+12 <= len(raw) && raw[i+6] == '\\' && raw[i+7] == 'u' {
				if lo, ok := hex4(i + 8); ok && lo >= 0xDC00 && lo <= 0xDFFF {
					i += 11
					continue
				}
			}
			return true
		case cp >= 0xDC00 && cp <= 0xDFFF:
			return true
		}
		i += 5
	}
	return false
}

// parseOrgJSON is parseJSON for an org definition, refusing (as unknown)
// the inputs Go and Node decode differently: bytes that are not valid
// UTF-8 (Node and Go replace them differently) and surrogate escapes.
func parseOrgJSON(raw []byte) (interface{}, error) {
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("%w: the file is not valid UTF-8", errUnknown)
	}
	if unpairedSurrogate(raw) {
		return nil, fmt.Errorf("%w: the file holds an unpaired \\u surrogate escape", errUnknown)
	}
	return parseJSON(raw)
}

func hashValue(v interface{}, dig digester) (string, error) {
	proj := projection(v)
	digests, err := instructionsDigests(v, dig.file)
	if err != nil {
		return "", err
	}
	blueprints, err := blueprintDigests(v, dig.blueprint)
	if err != nil {
		return "", err
	}
	if len(digests) > 0 || len(blueprints) > 0 {
		wrapped := map[string]interface{}{"definition": proj}
		if len(digests) > 0 {
			wrapped["instructions"] = digests
		}
		if len(blueprints) > 0 {
			wrapped["blueprints"] = blueprints
		}
		proj = wrapped
	}
	sum := sha256.Sum256([]byte(stringify(proj)))
	return hex.EncodeToString(sum[:]), nil
}

// projection is orgSignatureInput without the canonical sort (stringify
// orders keys itself).
func projection(v interface{}) interface{} {
	m, ok := v.(map[string]interface{})
	if !ok {
		return v
	}
	out := map[string]interface{}{}
	for k, val := range m {
		if unsignedOrgFields[k] {
			continue
		}
		if roles, isArr := val.([]interface{}); k == "roles" && isArr {
			rs := make([]interface{}, len(roles))
			for i, r := range roles {
				rs[i] = signedRole(r)
			}
			val = rs
		}
		out[k] = val
	}
	return out
}

func signedRole(r interface{}) interface{} {
	m, ok := r.(map[string]interface{})
	if !ok {
		return r
	}
	out := map[string]interface{}{}
	for k, val := range m {
		if !unsignedRoleFields[k] {
			out[k] = val
		}
	}
	return out
}

// forbiddenKeyPath is monomind's forbiddenKeyPath.
func forbiddenKeyPath(v interface{}, path string) string {
	switch t := v.(type) {
	case []interface{}:
		for i, e := range t {
			if hit := forbiddenKeyPath(e, fmt.Sprintf("%s[%d]", path, i)); hit != "" {
				return hit
			}
		}
	case map[string]interface{}:
		// Go maps lose the source order, so which forbidden key is reported
		// first can differ from monomind — never whether one is.
		for _, k := range propertyOrder(t) {
			here := k
			if path != "" {
				here = path + "." + k
			}
			if forbiddenKeys[k] {
				return here
			}
			if hit := forbiddenKeyPath(t[k], here); hit != "" {
				return hit
			}
		}
	}
	return ""
}

// roleOperatorDirNote says why an org role's process leaves the operator
// dir alone: monomind hides it from every role sandbox by design.
func roleOperatorDirNote(marker string) string {
	return marker + " is set (an org role or agent turn): the operator directory (" +
		"~/.monomind/orgrt-operator) is protected and not read here; only the operator signs and checks signatures"
}
