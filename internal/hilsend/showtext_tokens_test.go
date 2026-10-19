package hilsend

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Each shape the net reads, and what looks like one but isn't: phone
// numbers without a + (a date, a time, a decimal number and digits after a
// letter are none), long ids, UUIDs, Matrix ids and tel:, sip: and sms:
// URIs, addresses without a dot, IP literals, bare domains with a known
// TLD, and lower-case and 13-character Slack ids.
func TestTokenShapes(t *testing.T) {
	dash, nbsp := string(rune(0x2013)), string(rune(0xa0))
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"call 5551234567 or 555-123-4567, abroad 0044207946000; (555) 123 4567, 555" + dash + "123" + dash + "4567, 555" + nbsp + "123" + nbsp + "4567",
			[]string{"5551234567", "5551234567", "0044207946000", "5551234567", "5551234567", "5551234567"}},
		{"<@123456789012345678> gid 1201234567890123", []string{"123456789012345678", "1201234567890123"}},
		{"2026-10-19, 2026-10-19T08:30:00.123456789Z, 2026-10-19 12:30:00, 19.10.2026 08:00, 10-19-2026 1234, 12:34:56, 3.14159265, 1234567.89, 1.2.3, 123456, 1234567abc, v1234567, ID-1234567, id_1234567, $12345678, 1234567@, 123456789012345678901", nil},
		{"page d40e767c-d7af-4b18-a86d-55c61f1e39a4 urn:uuid:D40E767C-D7AF-4B18-A86D-55C61F1E39A4", []string{"d40e767c-d7af-4b18-a86d-55c61f1e39a4", "D40E767C-D7AF-4B18-A86D-55C61F1E39A4"}},
		{"@alice:matrix.org, #room:matrix.org and !abc123:matrix.org", []string{"@alice:matrix.org", "#room:matrix.org", "!abc123:matrix.org"}},
		{"tel:5551234567 sip:alice@pbx sms:+15551234567 SIPS:bob", []string{"5551234567", "+15551234567", "tel:5551234567", "sip:alice@pbx", "sms:+15551234567", "SIPS:bob"}},
		{"root@localhost, evil@mailhost", []string{"root@localhost", "evil@mailhost"}},
		{"10.0.0.1, 192.168.1.10:8080, 999.1.1.1, 2001:db8::1, [fe80::1]", []string{"10.0.0.1", "192.168.1.10", "2001:db8::1", "fe80::1"}},
		{"evil.com, mail.evil.co.uk/pay and Socket.IO. Not: e.g. i.e. node.js file.txt user.name main.py Mr.Smith end.Then", []string{"evil.com", "mail.evil.co.uk/pay", "Socket.IO"}},
		{"c0123abcd9 U0123ABCDEFGH. Not: download10 user12345 Cabcdefgh1 U0123ABCDEFGHI", []string{"c0123abcd9", "U0123ABCDEFGH"}},
	} {
		if got := addressTokens(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q:\n got %q\nwant %q", c.in, got, c.want)
		}
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

// Under a time-like key, 10 or 13 digits alone are a Unix time; the same
// digits under another key, and a phone number under a time-like key, are
// listed. Dates are never taken for phone numbers.
func TestATimeIsNoPhoneNumber(t *testing.T) {
	got, why := probe(hidden(map[string]interface{}{"createdAt": "1697712000", "ts": "1697712000123",
		"when": "2026-10-19T08:30:00.123456789Z", "zz": "1697712000", "end_time": "call 555-123-4567"}))
	for _, w := range []string{"\n  \"v[25].zz\": \"1697712000\"", "\n  \"v[25].end_time\": \"5551234567\""} {
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
