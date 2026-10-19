package hilsend

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func cardOf(v interface{}) string {
	return showText("op x/y", []string{"v"}, nil, nil, false, map[string]interface{}{"v": v})
}

func addresses(n int) []interface{} {
	var out []interface{}
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("a%02d@x.test", i))
	}
	return out
}

// A recipient-like key's value shows in full, up to showRecipientMax: every
// address of a list, then how many more, and one long string whole.
func TestARecipientValueShowsInFull(t *testing.T) {
	forty := append(addresses(39), "evil@x.test")
	if got := cardOf(map[string]interface{}{"to": forty}); !strings.Contains(got, "\n  \"to\":\n    [0]: \"a00@x.test\"") ||
		!strings.Contains(got, "\n    [39]: \"evil@x.test\"") || strings.Contains(got, "more elements") {
		t.Errorf("40 addresses: %q", got)
	}
	long := strings.Join(func() []string {
		var s []string
		for _, a := range addresses(60) {
			s = append(s, a.(string))
		}
		return append(s, "evil@x.test")
	}(), ", ")
	if got := cardOf(map[string]interface{}{"to": long}); !strings.Contains(got, "evil@x.test\"") || strings.Contains(got, "…") {
		t.Errorf("one long string: %q", got)
	}
	if got := cardOf(map[string]interface{}{"to": addresses(500)}); !strings.Contains(got, " more elements, 1 distinct domains: x.test …\n    [499]: \"a499@x.test\"") {
		t.Errorf("500 addresses, past the cap: %q", got[len(got)-120:])
	}
	// A list param whose own name is recipient-like lists every element too.
	got := showText("op x/y", []string{"additionalFields.bcc_emails"}, nil, nil, true,
		map[string]interface{}{"additionalFields": map[string]interface{}{"bcc_emails": addresses(30)}})
	if !strings.Contains(got, "\n  [29]: \"a29@x.test\"") || strings.Contains(got, "more elements") {
		t.Errorf("30 bcc addresses: %q", got)
	}
}

// A name and value pair whose name is recipient-like ({"name":
// "recipientEmail", "value": …}, as Salesforce passes its variables)
// counts as that field: its value comes first, in full, and an element
// past the tenth that has one is shown.
func TestANamedRecipientPairCounts(t *testing.T) {
	var list []interface{}
	for i := 0; i < 14; i++ {
		list = append(list, map[string]interface{}{"name": fmt.Sprintf("f%d", i), "value": i})
	}
	list = append(list, map[string]interface{}{"name": "recipientEmail", "value": "evil@x.test"})
	got := cardOf(list)
	if !strings.Contains(got, "\n  [14]:\n    \"value\" (\"recipientEmail\"): \"evil@x.test\"\n    \"name\": \"recipientEmail\"") {
		t.Errorf("the 15th pair: %q", got)
	}
	if strings.Contains(got, "[10]") || !strings.HasSuffix(got, "+4 more elements") {
		t.Errorf("the pairs that name no recipient: %q", got)
	}
}

// A value names the keys inside it down to showDepthMax levels, as paths,
// recipient-like first: a nested object, a list inside an object, and a
// list of lists.
func TestNestedNamesGoDown(t *testing.T) {
	pad := strings.Repeat("A", 600)
	deep := map[string]interface{}{"a": map[string]interface{}{"b": map[string]interface{}{"pad": pad, "to": "evil@x.test"}}}
	if got := cardOf(deep); !strings.Contains(got, "\n    keys: \"b.to\", \"b\", \"b.pad\"") {
		t.Errorf("a nested object: %q", got)
	}
	inList := map[string]interface{}{"list": []interface{}{map[string]interface{}{"pad": pad, "to": "evil@x.test"}}}
	if got := cardOf(inList); !strings.Contains(got, "\n    keys: \"[0].to\", \"[0].pad\"") {
		t.Errorf("a list inside an object: %q", got)
	}
	listOfLists := []interface{}{[]interface{}{map[string]interface{}{"pad": pad, "to": "evil@x.test"}}}
	if got := cardOf(listOfLists); !strings.Contains(got, "\n    keys: \"[0].to\", \"[0].pad\"") {
		t.Errorf("a list of lists: %q", got)
	}
	five := map[string]interface{}{"x": map[string]interface{}{"a": map[string]interface{}{"b": map[string]interface{}{"c": map[string]interface{}{"d": map[string]interface{}{"to": 1}}}}}}
	// Names stop at four levels; a recipient-like key is named, and its
	// value shown, at any depth.
	if got := cardOf(five); !strings.HasSuffix(got, "\n    keys: \"a.b.c.d.to\", \"a\", \"a.b\", \"a.b.c\", \"a.b.c.d\"\n    \"a.b.c.d.to\": 1") {
		t.Errorf("five levels down: %q", got)
	}
}

// Keys a lower-cased run, a word the first list missed, a fullwidth or a
// look-alike spelling still read as recipients; short words stay whole.
func TestRecipientLikeSeesRunOnsAndLookAlikes(t *testing.T) {
	for _, k := range []string{"sendto", "replyto", "recipientemail", "emailaddress", "dest", "destination", "target",
		"contact", "assignee", "owner", "member", "group", "audience", "t\u043e", "\uff54\uff4f", "recipient\u0415mail", "zz_to"} {
		if !recipientLike(k) {
			t.Errorf("%q is not taken for a recipient", k)
		}
	}
	for _, k := range []string{"total", "account", "token", "photos", "name", "status", "amount", "description", "pad"} {
		if recipientLike(k) {
			t.Errorf("%q is taken for a recipient", k)
		}
	}
}

// Each key is checked once per card, so a value with 100,000 keys, or
// 10,000 elements of 50, renders in well under a second.
func TestACardStaysQuickOnAHugeValue(t *testing.T) {
	wide := map[string]interface{}{}
	for i := 0; i < 100000; i++ {
		wide[fmt.Sprintf("field_%06d", i)] = i
	}
	var long []interface{}
	for i := 0; i < 10000; i++ {
		el := map[string]interface{}{}
		for j := 0; j < 50; j++ {
			el[fmt.Sprintf("k%02d", j)] = j
		}
		long = append(long, el)
	}
	for name, v := range map[string]interface{}{"100,000 keys": wide, "10,000 elements of 50 keys": long} {
		start := time.Now()
		if _, why := showCard("op x/y", []string{"v"}, nil, nil, false, map[string]interface{}{"v": v}); why != "" {
			t.Errorf("%s: unreviewable: %s", name, why)
		}
		if took := time.Since(start); took > 2*time.Second {
			t.Errorf("%s: %v", name, took)
		}
	}
}
