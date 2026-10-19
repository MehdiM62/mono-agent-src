package hilsend

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxAllowRecipient is the longest recipient an always-allow rule takes, in
// bytes: an email address's limit.
const MaxAllowRecipient = 320

// Actions are what a send can do, the actions an always-allow rule takes.
var Actions = []string{ActionEmail, ActionDM, ActionComment, ActionPost, ActionConnect, ActionPublish}

// CheckAllow says why a rule the operator writes by hand would be wrong, or
// nil: an action no send has, a recipient over MaxAllowRecipient bytes, one
// with * or % (a rule matches one recipient exactly, never a pattern), or
// one that reads like a label ("the ticket's requester"), which no send's
// recipient ever is. `hil allow add` runs it, so the app's form and every
// other caller go through it.
func CheckAllow(a Allow) error {
	if !slices.Contains(Actions, a.Action) {
		return fmt.Errorf("--action must be one of %s", strings.Join(Actions, ", "))
	}
	r := strings.TrimSpace(a.Recipient)
	if i := strings.IndexFunc(r, odd); i >= 0 {
		c, _ := utf8.DecodeRuneInString(r[i:])
		return fmt.Errorf("--recipient holds U+%04X, a control, invisible or non-breaking character no send's recipient has: write it as the card shows it", c)
	}
	switch {
	case r == "":
		return errors.New("--recipient is required")
	case len(r) > MaxAllowRecipient:
		return fmt.Errorf("--recipient is %d bytes; a recipient is at most %d", len(r), MaxAllowRecipient)
	case strings.ContainsAny(r, "*%"):
		return errors.New("--recipient can't hold * or %: a rule matches one recipient exactly, not a pattern")
	case LabelLike(r):
		return fmt.Errorf("--recipient %q reads like a description, not an address: write the profile URL, address, handle or name the send card shows", r)
	}
	return nil
}

// odd reports a character a recipient never holds: a control character
// (NUL, newline, tab), an invisible format character, or a space other than
// U+0020 (a non-breaking or a figure space).
func odd(c rune) bool {
	return unicode.IsControl(c) || unicode.Is(unicode.Cf, c) || (unicode.IsSpace(c) && c != ' ')
}

// LabelLike says whether a recipient reads like a label rather than an
// address, handle or name: after invisible format characters are dropped,
// it starts with "the " and holds no @ or digit. It is a courtesy: a label
// recipient matches no send anyway.
func LabelLike(r string) bool {
	v := strings.TrimSpace(strings.Map(func(c rune) rune {
		if unicode.Is(unicode.Cf, c) {
			return -1
		}
		return c
	}, r))
	return len(v) > 4 && strings.EqualFold(v[:4], "the ") && !strings.ContainsFunc(v, func(c rune) bool { return c == '@' || unicode.IsDigit(c) })
}
