package profile

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const maxProfileNameRunes = 64

var iconID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func (d Deps) validateIcon(icon string) error {
	if icon != "" && !iconID.MatchString(icon) {
		return d.InvalidInput("icon must be an id from the avatar manifest (lowercase letters, digits, - and _)")
	}
	return nil
}

// validateProfileName rejects a name that is empty, long, written with
// control, invisible, text-direction or unusual-space characters or path
// separators, that changes under compatibility normalization (ligatures,
// full-width or styled letters, ™, ², …), that mixes a look-alike script with
// another one, or that is "default". The caller has already put the name in
// canonical (NFC) form.
//
// The look-alike checks are a safeguard, not a proof: skeleton folds only the
// letters in confusable, so other look-alikes (dotless i, "rn" for "m", 0 for
// O, a trailing dot or digit, a space inside a word) are not caught.
func validateProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("profile name cannot be empty")
	}
	if utf8.RuneCountInString(name) > maxProfileNameRunes {
		return fmt.Errorf("profile name is longer than %d characters", maxProfileNameRunes)
	}
	for _, r := range name {
		switch {
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) ||
			(unicode.Is(unicode.Zs, r) && r != ' '):
			return fmt.Errorf("profile name cannot contain control, invisible or unusual space characters")
		case r == '/' || r == '\\':
			return fmt.Errorf("profile name cannot contain / or \\")
		}
	}
	if norm.NFKC.String(name) != name {
		return fmt.Errorf("profile name cannot contain characters that change under Unicode normalization (ligatures, full-width or styled letters, symbols such as ™ or ²): use the plain characters")
	}
	if mixesLookAlikeScript(name) {
		return fmt.Errorf("profile name cannot mix Cyrillic, Greek, Armenian, Georgian, Cherokee, Coptic or Lisu letters with letters of another script (look-alike letters hide as other names)")
	}
	if skeleton(name) == "default" {
		return fmt.Errorf("profile name cannot be \"default\": no profile is the default")
	}
	return nil
}

// lookAlikeScripts are the scripts with letters that pass for Latin ones.
// CJK, Arabic, Hebrew, Thai, Devanagari and the like have none, so they may
// stand next to Latin text ("Toyota トヨタ").
var lookAlikeScripts = []string{"Cyrillic", "Greek", "Armenian", "Georgian", "Cherokee", "Coptic", "Lisu"}

// mixesLookAlikeScript reports a name with letters of a look-alike script
// together with letters of any other script, Latin included.
func mixesLookAlikeScript(name string) bool {
	groups := map[string]bool{}
	for _, r := range name {
		if !unicode.IsLetter(r) {
			continue
		}
		g := "other"
		switch {
		case unicode.Is(unicode.Latin, r):
			g = "Latin"
		default:
			for _, s := range lookAlikeScripts {
				if unicode.Is(unicode.Scripts[s], r) {
					g = s
					break
				}
			}
		}
		groups[g] = true
	}
	for _, s := range lookAlikeScripts {
		if groups[s] && len(groups) > 1 {
			return true
		}
	}
	return false
}

// confusable maps letters that look like a Latin one to it (a short list of
// the Cyrillic and Greek ones, the usual look-alikes).
var confusable = map[rune]rune{
	'А': 'A', 'В': 'B', 'Е': 'E', 'К': 'K', 'М': 'M', 'Н': 'H', 'О': 'O', 'Р': 'P', 'С': 'C', 'Т': 'T', 'У': 'Y', 'Х': 'X', 'І': 'I', 'Ј': 'J', 'Ѕ': 'S',
	'а': 'a', 'е': 'e', 'о': 'o', 'р': 'p', 'с': 'c', 'у': 'y', 'х': 'x', 'і': 'i', 'ј': 'j', 'ѕ': 's', 'м': 'm', 'т': 't', 'к': 'k', 'н': 'h', 'в': 'b', 'һ': 'h', 'ԁ': 'd', 'ԛ': 'q', 'ԝ': 'w', 'ӏ': 'l',
	'Α': 'A', 'Β': 'B', 'Ε': 'E', 'Ζ': 'Z', 'Η': 'H', 'Ι': 'I', 'Κ': 'K', 'Μ': 'M', 'Ν': 'N', 'Ο': 'O', 'Ρ': 'P', 'Τ': 'T', 'Υ': 'Y', 'Χ': 'X',
	'ο': 'o', 'μ': 'm', 'ν': 'v', 'ρ': 'p', 'ι': 'i', 'κ': 'k', 'υ': 'u', 'χ': 'x', 'α': 'a', 'ε': 'e', 'γ': 'y', 'τ': 't',
}

// skeleton is a name as compared with other names: compatibility-normalized,
// look-alike letters replaced by the Latin ones they imitate, and lower-cased.
func skeleton(s string) string {
	var b strings.Builder
	for _, r := range norm.NFKC.String(s) {
		if m, ok := confusable[r]; ok {
			r = m
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// checkNameFree refuses a name that is, or looks the same as, the name or id
// of a profile other than except.
func (d Deps) checkNameFree(db *sql.DB, name, except string) error {
	rows, err := db.Query(`SELECT id, name FROM profiles WHERE id <> ?`, except)
	if err != nil {
		return fmt.Errorf("check profile name: %w", err)
	}
	defer rows.Close()
	want := skeleton(name)
	for rows.Next() {
		var id, other string
		if rows.Scan(&id, &other) == nil && (skeleton(other) == want || skeleton(id) == want) {
			return d.InvalidInput("%q is already, or looks the same as, the name or id of another profile", name)
		}
	}
	return rows.Err()
}
