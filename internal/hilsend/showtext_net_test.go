package hilsend

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// card is showCard over one param v.
func probe(v interface{}) (string, string) {
	return showCard("op x/y", []string{"v"}, nil, nil, false, map[string]interface{}{"v": v})
}

// A middle element of a long recipient list, or the middle of a long
// recipient string, can't hide behind the summary, however its domain
// sorts: every distinct domain of what it leaves out is named, and the
// address itself is in the net.
func TestAMiddleRecipientIsNamed(t *testing.T) {
	var list []interface{}
	var parts []string
	for i := 0; i < 1000; i++ {
		a := fmt.Sprintf("u%03d@d%02d.test", i, i%30)
		if i == 500 {
			a = "evil@zzz-attacker.test"
		}
		list = append(list, a)
		parts = append(parts, a)
	}
	for name, v := range map[string]interface{}{"a list": list, "a 15 KB string": strings.Join(parts, ", ")} {
		got, why := probe(map[string]interface{}{"to": v})
		if why != "" || !strings.Contains(got, "31 distinct domains: d00.test, d01.test, d02.test, d03.test, d04.test, d05.test, d06.test, d07.test, d08.test, d09.test, d10.test, d11.test, d12.test, d13.test, d14.test, d15.test, d16.test, d17.test, d18.test, d19.test, d20.test, d21.test, d22.test, d23.test, d24.test, d25.test, d26.test, d27.test, d28.test, d29.test, zzz-attacker.test") ||
			!strings.Contains(got, "\n  \"v.to") || !strings.Contains(got, ": \"evil@zzz-attacker.test\"") {
			t.Errorf("%s: unreviewable %q, card ends:\n%s", name, why, got[max(0, len(got)-600):])
		}
	}
}

// The net is the guarantee past the word lists: name and value pairs the
// classifier doesn't know, and keys it has no word for, at element or key
// 25 and after, show what they address; so does a plain list of strings
// under a param named for nothing.
func TestTheNetCatchesWhatTheWordsMiss(t *testing.T) {
	var elems []interface{}
	for i := 0; i < 25; i++ {
		elems = append(elems, map[string]interface{}{"k_x": i})
	}
	pairs := []map[string]interface{}{
		{"fieldName": "to", "fieldValue": "p1@evil.test"}, {"id": "recipient", "value": "p2@evil.test"},
		{"label": "To", "value": "p3@evil.test"}, {"property": "to", "value": "p4@evil.test"},
		{"name": "to", "stringValue": "p5@evil.test"}, {"name": "to", "values": []interface{}{"p6@evil.test"}},
		{"key": "to", "val": "p7@evil.test"}, {"n": "to", "v": "p8@evil.test"}, {"type": "recipient", "value": "p9@evil.test"},
	}
	for _, p := range pairs {
		elems = append(elems, p)
	}
	keys := map[string]interface{}{}
	for i := 0; i < 25; i++ {
		keys[fmt.Sprintf("aa_k%02d", i)] = i
	}
	names := []string{"sharedWith", "mentions", "viewers", "reviewers", "team", "sender", "number", "login", "ping", "rcpt",
		"rcpts", "recip", "tos", "kontakt", "t o"} // "number", "ping" and "t o" have no word: the net alone
	for i, n := range names {
		keys[n] = fmt.Sprintf("k%d@evil.test", i)
	}
	keys["principalId"] = float64(123456789)
	keys["subjectId"] = "987654321"
	var strs []interface{}
	for i := 0; i < 30; i++ {
		strs = append(strs, fmt.Sprintf("note %d", i))
	}
	strs[25] = "s25@evil.test"
	for name, c := range map[string]struct {
		v    interface{}
		want []string
	}{
		"pairs past element 25": {elems, []string{"p1@", "p2@", "p3@", "p4@", "p5@", "p6@", "p7@", "p8@", "p9@"}},
		"keys past key 25":      {keys, []string{"k0@", "k3@", "k8@", "k14@", "123456789", "987654321"}},
		"a list of strings":     {strs, []string{"s25@evil.test"}},
	} {
		got, why := probe(c.v)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %s not on the card", name, w)
			}
		}
		if why != "" {
			t.Errorf("%s: unreviewable: %s", name, why)
		}
	}
	for i := range names {
		if got, _ := probe(keys); !strings.Contains(got, fmt.Sprintf("k%d@evil.test", i)) {
			t.Errorf("key %q: its address isn't on the card", names[i])
		}
	}
}

// A pair the classifier doesn't know, past element 10, shows its value
// even when it is no address; and the words the review found join the
// list for sorting.
func TestAnUnknownPairShowsItsValue(t *testing.T) {
	var elems []interface{}
	for i := 0; i < 26; i++ {
		elems = append(elems, map[string]interface{}{"k_x": i})
	}
	elems = append(elems, map[string]interface{}{"label": "To", "value": "user-17"}, map[string]interface{}{"fieldName": "cc", "fieldValue": "team-9"})
	got, _ := probe(elems)
	if !strings.Contains(got, "\n  [26]:\n    \"value\" (\"To\"): \"user-17\"") || !strings.Contains(got, "\"fieldValue\" (\"cc\"): \"team-9\"") {
		t.Errorf("unknown pairs: %q", got)
	}
	for _, k := range []string{"sharedWith", "mentions", "viewers", "reviewers", "team", "sender", "login", "principalId", "rcpt", "recip", "tos", "kontakt"} {
		if !recipientLike(k) {
			t.Errorf("%q is not taken for a recipient", k)
		}
	}
}

// Copies and watchers by id (ccs, bccs, watchers) are recipients: their ids,
// past key 25, are on the card.
func TestCopiesAndWatchersByIDAreOnTheCard(t *testing.T) {
	keys := map[string]interface{}{}
	for i := 0; i < 25; i++ {
		keys[fmt.Sprintf("aa_k%02d", i)] = i
	}
	keys["ccs"] = "654321"
	keys["bccs"] = []interface{}{float64(765432)}
	keys["watchers"] = float64(7654321)
	got, why := probe(keys)
	for _, w := range []string{"654321", "765432", "7654321"} {
		if !strings.Contains(got, w) {
			t.Errorf("%s not on the card:\n%s", w, got)
		}
	}
	if why != "" {
		t.Errorf("unreviewable: %s", why)
	}
	for _, k := range []string{"ccs", "bccs", "watchers", "issueWatchers", "watcher_ids"} {
		if !recipientLike(k) {
			t.Errorf("%q is not taken for a recipient", k)
		}
	}
}

// What addresses someone has a shape: an email address, a link with a host,
// an @handle, a #channel, a Slack-style channel id, a phone number.
func TestAddressTokens(t *testing.T) {
	got := addressTokens("mail ada@x.test, see https://hooks.example.test/T1/B2 or ftp://files.test; ping @ada and #ops-team in C0123ABCD9, call +1 (555) 010-0199. Not: a@, #12, x://, C0123")
	want := []string{"+1555010", "0100199", "5550100199", "+15550100199", "ada@x.test", "https://hooks.example.test/T1/B2", "ftp://files.test", "@ada", "#ops-team", "C0123ABCD9"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tokens %q\nwant %q", got, want)
	}
}

// JSON text inside a string value, past its padding, is caught by its
// shapes: the address in it is on the card.
func TestJSONInsideAStringIsCaught(t *testing.T) {
	got, why := probe(map[string]interface{}{"notes": strings.Repeat("A", 300) + `{"to":"evil@x.test"}`})
	if why != "" || !strings.Contains(got, "\n  \"v.notes\": \"evil@x.test\"") {
		t.Errorf("unreviewable %q: %q", why, got)
	}
}

// More than fit on the card can't be approved from.
func TestTooManyAddressesForTheCard(t *testing.T) {
	var many []interface{}
	for i := 0; i < 30000; i++ {
		many = append(many, fmt.Sprintf("u%05d@d%05d.test", i, i))
	}
	got, why := probe(many)
	if !strings.HasSuffix(got, "\n(card cut)") || !strings.Contains(why, "cut where it shows") {
		t.Errorf("30,000 addresses: %d bytes, %q", len(got), why)
	}
}

// Raw JSON text over 8 MB isn't parsed, and a key over 256 bytes isn't
// normalised: either makes the card one that can't be approved from.
func TestOversizedInputIsRefused(t *testing.T) {
	raw := "[" + strings.Repeat("1,", 4<<20+1) + "1]"
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got, why := probe(raw)
	runtime.ReadMemStats(&after)
	if !strings.Contains(got, "too large to review: ") || !strings.Contains(why, "over 8 MB") {
		t.Errorf("9 MB of JSON text: %q, %q", got[:min(200, len(got))], why)
	}
	if mb := (after.TotalAlloc - before.TotalAlloc) >> 20; mb > 64 {
		t.Errorf("9 MB of JSON text took %d MB: it was parsed", mb)
	}
	long := strings.Repeat("\ufdfa", 100)
	if _, why := probe(map[string]interface{}{long: 1}); !strings.Contains(why, "a key of 300 bytes") {
		t.Errorf("a 300-byte key: %q", why)
	}
}

// An unreviewable card's approve with a text edit is refused for being
// unreviewable, before the edit is looked at.
func TestUnreviewableComesBeforeTheEdit(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	SetClassifier(fakeClassifier{})
	t.Cleanup(func() { SetClassifier(nil) })
	st := Store{db, "p1"}
	out, _, err := Guard(ctxFor(db), "app.fake", workflow.NodeInput{}, map[string]interface{}{"operation": "raw",
		"updateFields": map[string]interface{}{"zz": make([]interface{}, showParamKeysMax+1)}})
	if err != nil {
		t.Fatal(err)
	}
	text := "edited"
	if _, err := Resolve(context.Background(), st, out[0].Items[0].JSON["hil_id"].(string), true, Edit{Text: &text}, false, (&fakeRunner{}).run); !errors.Is(err, ErrUnreviewable) {
		t.Errorf("approve with an edit: %v", err)
	}
}
