package hilsend

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Each shape the net reads, and what looks like one but isn't: phone
// numbers without a + and glued to a word on one side (a valid date, a
// time, a decimal number and digits inside a word are none; an invalid
// date is digits), long ids, fullwidth and Arabic-Indic digits, UUIDs with
// and without dashes, Matrix ids and tel:, sip: and sms: URIs, addresses
// without a dot, IP literals, bare domains, and lower-case and
// 13-character Slack ids.
func TestTokenShapes(t *testing.T) {
	dash, nbsp := string(rune(0x2013)), string(rune(0xa0))
	digits := func(zero rune, s string) string {
		return strings.Map(func(r rune) rune { return zero + r - '0' }, s)
	}
	word := func(rs ...rune) string { return string(rs) }
	idn := "m" + word(0xfc) + "nchen.de " + word(0x43f, 0x440, 0x438, 0x43c, 0x435, 0x440) + "." + word(0x440, 0x444)
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"call 5551234567 or 555-123-4567, abroad 0044207946000; (555) 123 4567, 555" + dash + "123" + dash + "4567, 555" + nbsp + "123" + nbsp + "4567",
			[]string{"5551234567", "1234567", "0044207946000"}},
		{"<@123456789012345678> gid 1201234567890123", []string{"123456789012345678", "1201234567890123"}},
		{"2026-10-19, 2026-10-19T08:30:00.123456789Z, 2026-10-19 12:30:00, 19.10.2026 08:00, 10-19-2026 1234, 20261019T083000Z, 12:34:56, 3.14159265, 1234567.89, 1.2.3, 123456, ab1234567cd, 123456789012345678901", nil},
		{"5551-23-45", []string{"55512345"}},
		{"9999-99-99", []string{"99999999"}},
		{"tel5551234567", []string{"5551234567"}},
		{"ph-5551234567", []string{"5551234567"}},
		{"$5551234567", []string{"5551234567"}},
		{"5551234567ext", []string{"5551234567"}},
		{digits(0xff10, "5551234567") + " " + digits(0x660, "5559876543") + " " + digits(0x6f0, "5550001111"), []string{"5551234567", "5559876543", "5550001111"}},
		{"page d40e767c-d7af-4b18-a86d-55c61f1e39a4 urn:uuid:D40E767C-D7AF-4B18-A86D-55C61F1E39A4", []string{"d40e767c-d7af-4b18-a86d-55c61f1e39a4", "D40E767C-D7AF-4B18-A86D-55C61F1E39A4"}},
		{"d40e767cd7af4b18a86d55c61f1e39a4. Not: da39a3ee5e6b4b0d3255bfef95601890afd80709", []string{"d40e767cd7af4b18a86d55c61f1e39a4"}},
		{"@alice:matrix.org, #room:matrix.org and !abc123:matrix.org", []string{"@alice:matrix.org", "#room:matrix.org", "!abc123:matrix.org"}},
		{"tel:5551234567 sip:alice@pbx sms:+15551234567 SIPS:bob", []string{"5551234567", "+15551234567", "tel:5551234567", "sip:alice@pbx", "sms:+15551234567", "SIPS:bob"}},
		{"root@localhost, evil@mailhost", []string{"root@localhost", "evil@mailhost"}},
		{"10.0.0.1, 192.168.1.10:8080, 999.1.1.1, 2001:db8::1, [fe80::1]", []string{"10.0.0.1", "192.168.1.10", "2001:db8::1", "fe80::1"}},
		{"evil.com, mail.evil.co.uk/pay and Socket.IO", []string{"evil.com", "mail.evil.co.uk/pay", "Socket.IO"}},
		{"evil.zip clip.mov me.social x.online y.site z.tk a.cc b.in c.us d.me e.to printer.local db.internal nas.lan " + idn + " xn--80ak6aa92e.com localhost:8080 //intranet/hook",
			strings.Fields("evil.zip clip.mov me.social x.online y.site z.tk a.cc b.in c.us d.me e.to printer.local db.internal nas.lan " + idn + " xn--80ak6aa92e.com localhost:8080 //intranet/hook")},
		{"Hello. Then we go, e.g. Mr. Smith, i.e. at 3.5 p.m. See README.md, main.go, app.py, index.html, data.json, photo.jpg, report.pdf and console.log(x) or this.userName. Note: 12 items, ratio 3:4, step 2/3.", nil},
		{"c0123abcd9 U0123ABCDEFGH. Not: download10 user12345 Cabcdefgh1 U0123ABCDEFGHI", []string{"c0123abcd9", "U0123ABCDEFGH"}},
	} {
		if got := addressTokens(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q:\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

// Numbers side by side are each read, apart by a space, a tab, a newline
// or four separators and more, a + on one of them or not; and an 8 MB run
// of distinct ones can't be approved from.
func TestNumbersSideBySide(t *testing.T) {
	for in, want := range map[string][]string{
		"5551234567 5559876543":      {"5551234567", "5559876543"},
		"5551234567\n5559876543":     {"5551234567", "5559876543"},
		"5551234567\t5559876543":     {"5551234567", "5559876543"},
		"555-123-4567 555-987-6543":  {"5551234567", "5559876543"},
		"555-123-4567\n555-987-6543": {"5551234567", "5559876543"},
		"5551234 5559876 5550000":    {"5551234", "5559876", "5550000"},
		"5551234\t5559876\t5550000":  {"5551234", "5559876", "5550000"},
		"+15551234567 5559876543":    {"+15551234567", "5559876543"},
		"5551234567----5559876543":   {"5551234567", "5559876543"},
		"5551234567 -- - 5559876543": {"5551234567", "5559876543"},
		"5551234567-5559876543":      {"5551234567", "5559876543"},
		"11 22 33 44 55 66 77 88":    {"112233445566", "223344556677", "334455667788"},
		"+33 6 12 34 56 78":          {"+33612345678", "612345678", "12345678"},
	} {
		got := addressTokens(in)
		for _, w := range want {
			if !slices.Contains(got, w) {
				t.Errorf("%q: %s not read, got %q", in, w, got)
			}
		}
	}
	var b strings.Builder
	for i := 0; b.Len() < 8<<20; i++ {
		fmt.Fprintf(&b, "%08d ", 10000000+i)
	}
	if got, why := probe(map[string]interface{}{"notes": b.String()}); why == "" {
		t.Errorf("8 MB of distinct numbers: approvable, %d bytes", len(got))
	}
}

// hidden is v as element 25 of a list whose elements past the tenth the
// card leaves out: none holds a recipient-like key.
func hidden(v map[string]interface{}) []interface{} {
	var list []interface{}
	for i := 0; i < 25; i++ {
		list = append(list, map[string]interface{}{"k_x": i})
	}
	return append(list, v)
}

// Under a time-like key, 10 or 13 digits alone that read as a time from
// 2001 to 2100 are a Unix time; the same digits under another key, a phone
// number under a time-like key, and 10 or 13 digits past 2100 are listed.
// Dates are never taken for phone numbers.
func TestATimeIsNoPhoneNumber(t *testing.T) {
	got, why := probe(hidden(map[string]interface{}{"createdAt": "1697712000", "ts": "1697712000123",
		"when": "2026-10-19T08:30:00.123456789Z", "zz": "1697712000", "end_time": "call 555-123-4567",
		"expires_at": "5559876543", "start": "9999999999999"}))
	for _, w := range []string{"\n  \"v[25].zz\": \"1697712000\"", "\n  \"v[25].end_time\": \"5551234567\"",
		"\n  \"v[25].expires_at\": \"5559876543\"", "\n  \"v[25].start\": \"9999999999999\""} {
		if !strings.Contains(got, w) {
			t.Errorf("%s isn't listed", w)
		}
	}
	for _, w := range []string{"createdAt", "1697712000123", "when\""} {
		if strings.Contains(got, w) {
			t.Errorf("%s is listed", w)
		}
	}
	if why != "" {
		t.Errorf("unreviewable: %s", why)
	}
}

// An address hidden in a recipient-like field is listed with its path even
// when another field such as notes shows it: there it reads as no
// recipient. One a recipient-like field shows isn't listed again.
func TestARecipientHiddenInItsFieldIsListed(t *testing.T) {
	to := addresses(600)
	to[500] = "evil@x.test"
	got, _ := probe(map[string]interface{}{"notes": "write to evil@x.test", "to": to})
	if !strings.Contains(got, "\n  \"v.to[500]\": \"evil@x.test\"") {
		t.Errorf("evil@x.test, shown in notes, isn't listed under to:\n%s", got[max(0, len(got)-400):])
	}
	for name, v := range map[string]map[string]interface{}{
		"in to's head": {"notes": "write to evil@x.test", "to": append([]interface{}{"evil@x.test"}, to...)},
		"in cc":        {"cc": []interface{}{"evil@x.test"}, "to": to},
	} {
		if got, _ := probe(v); strings.Contains(got, "]\": \"evil@x.test\"") {
			t.Errorf("%s: listed again:\n%s", name, got[max(0, len(got)-400):])
		}
	}
}

// A card cut before the net, where it shows no recipient, still can't be
// approved from when the net has something to list: the address in a
// param past the cut is out of sight.
func TestACardCutElsewhereStillNeedsItsNet(t *testing.T) {
	config := map[string]interface{}{"zz": "mail evil@x.test"}
	var show []string
	for i := 0; i < 600; i++ {
		p := fmt.Sprintf("p%03d", i)
		config[p] = strings.Repeat("a", 600)
		show = append(show, p)
	}
	got, why := showCard("op x/y", append(show, "zz"), nil, nil, true, config)
	if !strings.HasSuffix(got, "\n(card cut)") || why == "" {
		t.Errorf("cut at %d bytes, unreviewable %q", len(got), why)
	}
}

// The net reads the card's text as its JSON escapes stand for: an address
// in angle brackets, a link with &, and an address after a line break are
// on the card, and aren't listed again.
func TestEscapedTextIsSeen(t *testing.T) {
	got, why := probe(map[string]interface{}{"to": "Ada <ada@x.test>", "notes": "Thanks,\nbob@x.test & co: https://x.test/?a=1&b=2"})
	if why != "" || strings.Contains(got, "Also in what it sends") {
		t.Errorf("unreviewable %q, card:\n%s", why, got)
	}
}

// A number of 6 digits or more is an id next to a recipient-like key, in
// a string under an id key, and after an id key in JSON text inside a
// string: each is listed when the card leaves it out, and not when it
// shows it.
func TestSixDigitIDs(t *testing.T) {
	near := map[string]interface{}{"to": "a@x.test", "zz_ref": "123456", "zz_num": float64(234567)}
	for i := 0; i < 25; i++ {
		near[fmt.Sprintf("k%02d", i)] = i
	}
	got, _ := probe(near)
	for _, w := range []string{"\n  \"v.zz_ref\": \"123456\"", "\n  \"v.zz_num\": \"234567\""} {
		if !strings.Contains(got, w) {
			t.Errorf("next to to: %s isn't listed", w)
		}
	}
	delete(near, "to")
	if got, _ := probe(near); strings.Contains(got, "123456") {
		t.Errorf("no recipient-like key: 123456 is listed")
	}
	got, _ = probe(map[string]interface{}{"ref_ids": "[" + strings.Repeat("1, ", 70) + "123456, -100234567]",
		"meta": strings.Repeat("A", 300) + `{"chat_id": 654321}`})
	for _, w := range []string{"\n  \"v.ref_ids\": \"123456\"", "\n  \"v.ref_ids\": \"-100234567\"", "\n  \"v.meta\": \"654321\""} {
		if !strings.Contains(got, w) {
			t.Errorf("%s isn't listed:\n%s", w, got)
		}
	}
	if got, _ := probe(map[string]interface{}{"chat_id": float64(123456), "user_id": "-1001234567890"}); strings.Contains(got, "Also in what it sends") {
		t.Errorf("shown ids listed again:\n%s", got)
	}
}
