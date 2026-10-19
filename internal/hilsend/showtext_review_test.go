package hilsend

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// Decoys can't spend the real recipient's room: past eight recipient-like
// keys each gets 512 bytes, and a list always shows its last element and
// names the domains it leaves out, so twenty padded bcc_NN keys sorted
// first don't hide the "to" list's last address.
func TestDecoysDontHideTheRealRecipient(t *testing.T) {
	obj := map[string]interface{}{"to": append(addresses(59), "evil@evil.test")}
	for i := 0; i < 20; i++ {
		obj[fmt.Sprintf("bcc_%02d", i)] = strings.Repeat("A", 2000) + " x@hidden.test " + strings.Repeat("A", 2000)
	}
	got, why := showCard("op x/y", []string{"v"}, nil, nil, false, map[string]interface{}{"v": obj})
	if why != "" || !strings.Contains(got, "\n    [4]: \"a04@x.test\"") || !strings.Contains(got, " more elements, 1 distinct domains: x.test …\n    [59]: \"evil@evil.test\"") {
		t.Errorf("unreviewable %q, card:\n%s", why, got)
	}
	if !strings.Contains(got, "\n  \"bcc_00\": \"AAA") || !strings.Contains(got, "A … 3505 more bytes, 1 distinct domains: hidden.test … AAA") {
		t.Errorf("a decoy's share: %q", got[:800])
	}
}

// Every key is checked, per param: thousands of junk keys don't push a
// recipient out, in the same param or in another one. Past a million keys
// and elements a param is too large to review, and the card says so.
func TestJunkKeysDontHideARecipient(t *testing.T) {
	junk := manyJunk(6000)
	junk["zz_to"] = "evil@x.test"
	if got := cardOf(junk); !strings.Contains(got, "\n  \"zz_to\": \"evil@x.test\"") {
		t.Errorf("6,000 junk keys, then zz_to: %q", got[:300])
	}
	got := showText("salesforce flow/invoke", []string{"variablesUi", "variablesJson"}, nil, nil, false,
		map[string]interface{}{"variablesUi": manyJunk(6000), "variablesJson": `{"to":"evil@x.test"}`})
	if !strings.Contains(got, "\nvariablesJson:\n  \"to\": \"evil@x.test\"") {
		t.Errorf("6,000 junk names in one param, the recipient in the next: %q", got[len(got)-200:])
	}
	huge := make([]interface{}, showParamKeysMax+1)
	got, why := showCard("op x/y", []string{"v"}, nil, nil, false, map[string]interface{}{"v": huge})
	if !strings.Contains(got, "\nv: too large to review: more than 1000000 keys and elements") || !strings.Contains(why, "too large to review") {
		t.Errorf("a million and one elements: %q, %q", got, why)
	}
}

func manyJunk(n int) map[string]interface{} {
	m := map[string]interface{}{}
	for i := 0; i < n; i++ {
		m[fmt.Sprintf("aa_junk_%05d", i)] = i
	}
	return m
}

// A recipient-like value nested in another value shows in full too, at any
// depth: a "to" list two levels down shows its last address.
func TestANestedRecipientShowsInFull(t *testing.T) {
	v := map[string]interface{}{"meta": map[string]interface{}{"b": map[string]interface{}{"to": append(addresses(39), "evil@x.test")}}}
	if got := cardOf(v); !strings.Contains(got, "\n    \"b.to\":\n      [0]: \"a00@x.test\"") || !strings.Contains(got, "\n      [39]: \"evil@x.test\"") {
		t.Errorf("a nested to list: %q", got)
	}
}

// A card stops at 256 KB with "(card cut)"; cut where it shows a recipient,
// it can't be approved from, and cut elsewhere it can.
func TestACardIsCutAt256KB(t *testing.T) {
	var many []interface{}
	for i := 0; i < 20000; i++ {
		many = append(many, map[string]interface{}{"to": fmt.Sprintf("a%05d@x.test", i), "pad": strings.Repeat("p", 150)})
	}
	got, why := showCard("op x/y", []string{"v"}, nil, nil, false, map[string]interface{}{"v": many})
	if len(got) > showCardMax+100 || !strings.HasSuffix(got, "\n(card cut)") || !strings.Contains(why, "cut where it shows a field that may name a recipient") {
		t.Errorf("20,000 recipients: %d bytes, %q", len(got), why)
	}
	var wide []interface{}
	for i := 0; i < showElemsMax; i++ {
		el := map[string]interface{}{}
		for j := 0; j < showKeysMax; j++ {
			nested := map[string]interface{}{}
			for k := 0; k < 200; k++ {
				nested[fmt.Sprintf("field_%03d", k)] = k
			}
			el[fmt.Sprintf("k%02d", j)] = nested
		}
		wide = append(wide, el)
	}
	got, why = showCard("op x/y", []string{"v"}, nil, nil, false, map[string]interface{}{"v": wide})
	if !strings.HasSuffix(got, "\n(card cut)") || why != "" {
		t.Errorf("no recipient, cut: %d bytes, ends %q, %q", len(got), got[len(got)-30:], why)
	}
}

// A format character inside a key doesn't hide it, and recipient words of
// other languages count.
func TestRecipientLikeSeesFormatCharactersAndOtherLanguages(t *testing.T) {
	for _, k := range []string{"t\u200bo", "e\u2060mail", "\u6536\u4ef6\u4eba", "Empf\u00e4nger", "destinataire", "destinatario",
		"\u0e2a\u0e48\u0e07\u0e16\u0e36\u0e07", "\u043f\u043e\u043b\u0443\u0447\u0430\u0442\u0435\u043b\u044c",
		"\u041f\u043e\u043b\u0443\u0447\u0430\u0442\u0435\u043b\u044c", "\uc218\uc2e0\uc790"} {
		if !recipientLike(k) {
			t.Errorf("%q is not taken for a recipient", k)
		}
	}
}

// An unreviewable card can be rejected, never approved: approve and
// always-allow are refused with ErrUnreviewable, nothing runs, and it stays
// pending.
func TestAnUnreviewableCardCantBeApproved(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	SetClassifier(fakeClassifier{})
	t.Cleanup(func() { SetClassifier(nil) })
	st := Store{db, "p1"}
	out, handled, err := Guard(ctxFor(db), "app.fake", workflow.NodeInput{}, map[string]interface{}{"operation": "raw",
		"updateFields": map[string]interface{}{"zz": make([]interface{}, showParamKeysMax+1)}})
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	id := out[0].Items[0].JSON["hil_id"].(string)
	p, err := st.Get(ctxFor(db), id)
	if err != nil || !strings.Contains(p.Unreviewable, "too large to review") || !strings.Contains(p.Text, "too large to review") {
		t.Fatalf("payload: %q, %q, %v", p.Unreviewable, p.Text, err)
	}
	f := &fakeRunner{}
	if _, err := Resolve(context.Background(), st, id, true, Edit{}, false, f.run); !errors.Is(err, ErrUnreviewable) || len(f.calls) != 0 {
		t.Errorf("approve: %v, %d run(s)", err, len(f.calls))
	}
	// This card's recipient is a label, so always-allow is refused before
	// the claim; the claim refuses it for an unreviewable card too.
	if _, _, _, claimed, err := st.claim(context.Background(), id, true); !errors.Is(err, ErrUnreviewable) || claimed {
		t.Errorf("claim with always-allow: %v", err)
	}
	if p, _ := st.Get(ctxFor(db), id); p.State != StatePending {
		t.Fatalf("after the refused approvals: %s", p.State)
	}
	if p, err := Resolve(context.Background(), st, id, false, Edit{}, false, f.run); err != nil || p.State != StateRejected {
		t.Errorf("reject: %s, %v", p.State, err)
	}
	// A recipient a rule could stand for (a ticket, not a label): Resolve
	// refuses always-allow too, stores no rule and runs nothing.
	out, handled, err = Guard(ctxFor(db), "app.fake", workflow.NodeInput{}, map[string]interface{}{"operation": "rawreply", "ticket": "T-1",
		"updateFields": map[string]interface{}{"publicReply": "Done", "zz": make([]interface{}, showParamKeysMax+1)}})
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	id = out[0].Items[0].JSON["hil_id"].(string)
	if p, _ := st.Get(ctxFor(db), id); p.Unreviewable == "" || p.Recipient.Label || p.Recipient.Name != "T-1" {
		t.Fatalf("the ticket's card: unreviewable %q, recipient %+v", p.Unreviewable, p.Recipient)
	}
	if _, err := Resolve(context.Background(), st, id, true, Edit{}, true, f.run); !errors.Is(err, ErrUnreviewable) || len(f.calls) != 0 {
		t.Errorf("always-allow: %v, %d run(s)", err, len(f.calls))
	}
	if rules, err := st.Allows(context.Background()); err != nil || len(rules) != 0 {
		t.Errorf("always-allow stored %d rule(s): %v", len(rules), err)
	}
	if p, _ := st.Get(ctxFor(db), id); p.State != StatePending {
		t.Errorf("after the refused always-allow: %s", p.State)
	}
}
