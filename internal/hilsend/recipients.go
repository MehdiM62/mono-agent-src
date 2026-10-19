package hilsend

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// recipientWords are the words of a key that may say whom a send reaches
// (internal/apipkg's send lint takes the same ones for a recipient param).
var recipientWords = map[string]bool{"to": true, "cc": true, "bcc": true, "email": true, "emails": true, "mail": true,
	"mailto": true, "recipient": true, "recipients": true, "phone": true, "mobile": true, "tel": true, "telephone": true,
	"sms": true, "msisdn": true, "whatsapp": true, "channel": true, "channels": true, "chat": true, "user": true,
	"users": true, "url": true, "uri": true, "webhook": true, "endpoint": true, "address": true, "addresses": true,
	"host": true, "receiver": true, "receivers": true, "addressee": true, "addressees": true, "attendee": true,
	"attendees": true, "invitee": true, "invitees": true, "guest": true, "guests": true, "participant": true,
	"participants": true, "subscriber": true, "subscribers": true, "invite": true, "invites": true, "invitation": true,
	"notify": true, "notification": true, "notifications": true, "welcome": true, "activate": true, "activation": true,
	"dest": true, "destination": true, "destinations": true, "target": true, "targets": true, "contact": true,
	"contacts": true, "assignee": true, "assignees": true, "owner": true, "owners": true, "member": true,
	"members": true, "group": true, "groups": true, "audience": true, "audiences": true, "shared": true,
	"mention": true, "mentions": true, "viewer": true, "viewers": true, "reviewer": true, "reviewers": true,
	"team": true, "teams": true, "sender": true, "senders": true, "login": true, "principal": true,
	"principals": true, "rcpt": true, "rcpts": true, "recip": true, "recips": true, "tos": true, "kontakt": true,
	"kontakte": true, "ccs": true, "bccs": true, "watcher": true, "watchers": true}

// recipientWholeOnly are recipient words matched as whole words only: inside
// a longer one they match too much (tos in "photos", team in "steam", recip
// in "recipe").
var recipientWholeOnly = map[string]bool{"tos": true, "team": true, "teams": true, "recip": true, "recips": true,
	"shared": true, "login": true, "rcpt": true, "rcpts": true}

// recipientWordsElsewhere are recipient words of other languages: Chinese,
// Japanese, Korean, German, French, Spanish, Portuguese, Italian, Dutch,
// Swedish, Polish, Turkish, Russian, Ukrainian, Greek, Arabic, Hebrew,
// Hindi and Thai ones for recipient, addressee or send to.
var recipientWordsElsewhere = []string{"收件人", "收件者", "收信人", "接收者", "受信者", "宛先", "受取人", "수신자", "받는사람",
	"empfänger", "empfaenger", "adressat", "destinataire", "destinataires", "destinatario", "destinatarios",
	"destinatário", "destinatários", "ontvanger", "mottagare", "odbiorca", "adresat", "alıcı", "получатель",
	"получатели", "адресат", "одержувач", "παραλήπτης", "المستلم", "مستلم", "נמען", "प्राप्तकर्ता", "ส่งถึง", "ผู้รับ"}

func init() {
	for _, w := range recipientWordsElsewhere {
		recipientWords[strings.ToLower(recipientSkeleton(w))] = true
	}
}

// recipientInner are the recipient words also matched inside a longer run
// of letters (recipientemail, emailaddress): all but the two-letter ones,
// which would match too much (to in "total", cc in "account"); those match
// at the end of one only (sendto, replyto, mailcc).
var recipientInner []string

func init() {
	for w := range recipientWords {
		if len(w) > 2 && !recipientWholeOnly[w] {
			recipientInner = append(recipientInner, w)
		}
	}
}

// recipientLike reports whether key k may say whom a send reaches: one of
// its words (camelCase, snake_case, kebab-case) is a recipient word, or,
// in a key of 6 letters or more, a recipient word of 3 or more is inside
// it, or it ends in "to" or "cc". Compatibility forms (fullwidth letters)
// and Cyrillic and Greek look-alikes count as the Latin letters they show.
func recipientLike(k string) bool {
	words := nameWords(recipientSkeleton(k))
	for _, w := range words {
		if recipientWords[w] {
			return true
		}
	}
	flat := strings.Join(words, "")
	if len(flat) < 6 {
		return false
	}
	if strings.HasSuffix(flat, "to") || strings.HasSuffix(flat, "cc") {
		return true
	}
	for _, w := range recipientInner {
		if strings.Contains(flat, w) {
			return true
		}
	}
	return false
}

// RecipientLike is recipientLike, for the sends lint
// (internal/noderegistry).
func RecipientLike(k string) bool { return recipientLike(k) }

// recipientSkeleton is k without format characters (a zero-width space
// between "t" and "o"), compatibility-normalized, with look-alike letters
// replaced by the Latin ones they imitate.
func recipientSkeleton(k string) string {
	k = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, k)
	return strings.Map(func(r rune) rune {
		if m, ok := lookAlike[r]; ok {
			return m
		}
		return r
	}, norm.NFKC.String(k))
}

// nameWords is k's words, lower-cased: split at a lower-case letter or digit
// followed by an upper-case one, and at anything but letters, marks (Thai
// and Hindi vowels) and digits.
func nameWords(k string) []string {
	var words []string
	var cur strings.Builder
	prev := rune(0)
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, strings.ToLower(cur.String()))
			cur.Reset()
		}
	}
	for _, r := range k {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsMark(r) && !unicode.IsDigit(r):
			flush()
		case unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)):
			flush()
			cur.WriteRune(r)
		default:
			cur.WriteRune(r)
		}
		prev = r
	}
	flush()
	return words
}

// lookAlike maps letters that look like a Latin one to it: the usual
// Cyrillic and Greek ones (internal/cli/profile's profile names use the same
// list).
var lookAlike = map[rune]rune{
	'А': 'A', 'В': 'B', 'Е': 'E', 'К': 'K', 'М': 'M', 'Н': 'H', 'О': 'O', 'Р': 'P', 'С': 'C', 'Т': 'T', 'У': 'Y', 'Х': 'X', 'І': 'I', 'Ј': 'J', 'Ѕ': 'S',
	'а': 'a', 'е': 'e', 'о': 'o', 'р': 'p', 'с': 'c', 'у': 'y', 'х': 'x', 'і': 'i', 'ј': 'j', 'ѕ': 's', 'м': 'm', 'т': 't', 'к': 'k', 'н': 'h', 'в': 'b', 'һ': 'h', 'ԁ': 'd', 'ԛ': 'q', 'ԝ': 'w', 'ӏ': 'l',
	'Α': 'A', 'Β': 'B', 'Ε': 'E', 'Ζ': 'Z', 'Η': 'H', 'Ι': 'I', 'Κ': 'K', 'Μ': 'M', 'Ν': 'N', 'Ο': 'O', 'Ρ': 'P', 'Τ': 'T', 'Υ': 'Y', 'Χ': 'X',
	'ο': 'o', 'μ': 'm', 'ν': 'v', 'ρ': 'p', 'ι': 'i', 'κ': 'k', 'υ': 'u', 'χ': 'x', 'α': 'a', 'ε': 'e', 'γ': 'y', 'τ': 't',
}
