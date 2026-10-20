package hilsend

import (
	"context"
	"slices"
	"testing"

	"github.com/monoes/mono-agent/internal/connections"
)

func TestKeyWordsSplitAKeyLikeAReader(t *testing.T) {
	for k, want := range map[string][]string{
		"ccEmail": {"cc", "email"}, "bccEmail": {"bcc", "email"}, "bcc_address": {"bcc", "address"}, "bccAddress": {"bcc", "address"},
		"CCAddress": {"cc", "address"}, "additional_emails": {"additional", "emails"}, "additionalFields": {"additional", "fields"},
		"Reply-To": {"reply", "to"}, "BCC": {"bcc"}, "\uff42\uff43\uff43": {"bcc"}, "bcc\u200b": {"bcc"}, "to2": {"to2"},
	} {
		if got := keyWords(k); !slices.Equal(got, want) {
			t.Errorf("keyWords(%q) = %q, want %q", k, got, want)
		}
	}
	for _, k := range []string{"ccEmail", "bccEmail", "bccAddress", "bcc_address", "Bcc", "CC", "ccRecipients", "additional_emails", "attendees", "recipients",
		"ccs", "Bccs", "watchers", "watcherIds", "add_watcher", "notificationRecipientsUi", "recipientsUi", "recipients_values"} {
		if !copyKey(k) {
			t.Errorf("copyKey(%q) = false", k)
		}
	}
	for _, k := range []string{"account", "access", "success", "occupation", "accent", "subject", "to"} {
		if copyKey(k) {
			t.Errorf("copyKey(%q) = true", k)
		}
	}
}

// A copy under any key with a word cc or bcc, and an address anywhere but
// the text, the sender and the one recipient, gets the card (third security
// review of #871). Mailgun, SendGrid, Mailjet and Mandrill name their copies
// ccEmail, bccEmail and bccAddress, Freshservice additional_emails.
func TestEveryCopyAndStrayAddressGetsACard(t *testing.T) {
	st := allowFor(t, "comm.email_send", emailConfig())
	if h, _ := held(t, st, "comm.email_send", emailConfig()); h {
		t.Fatal("the plain send to the allowed recipient was held")
	}
	const evil = "evil@attacker.test"
	nested := func(head, key string) map[string]interface{} {
		return with(emailConfig(), head, map[string]interface{}{key: evil})
	}
	for name, cfg := range map[string]map[string]interface{}{
		"ccEmail":                     with(emailConfig(), "ccEmail", evil),
		"bccEmail":                    with(emailConfig(), "bccEmail", evil),
		"bccAddress":                  with(emailConfig(), "bccAddress", evil),
		"bcc_address":                 with(emailConfig(), "bcc_address", evil),
		"options.bcc_address":         nested("options", "bcc_address"),
		"additionalFields.ccEmail":    nested("additionalFields", "ccEmail"),
		"additionalFields.bccEmail":   nested("additionalFields", "bccEmail"),
		"additionalFields.bccAddress": nested("additionalFields", "bccAddress"),
		"additional_emails":           with(emailConfig(), "additional_emails", []interface{}{evil}),
		"forward_to (no copy word)":   with(emailConfig(), "forward_to", evil),
		"envelope.to":                 with(emailConfig(), "envelope", map[string]interface{}{"to": []interface{}{evil}}),
		"a header":                    nested("headers", "X-Forward"),
		"a note":                      with(emailConfig(), "notes", "also tell "+evil),
		"a phone number":              with(emailConfig(), "sms_backup", "+1 555 0100 999"),
		"another server":              with(emailConfig(), "smtp_host", "smtp.attacker.test"),
		// Every shape an address or a number takes (security review of #871
		// round 3: the net read ASCII emails and +phones only).
		"a Unicode address":     with(emailConfig(), "notes", "\u00fc@ex\u00e4mple.de"),
		"a dotless domain":      with(emailConfig(), "notes", "evil@intranet"),
		"a bracketed IP":        with(emailConfig(), "notes", "evil@[10.0.0.1]"),
		"a bracketed IPv6":      with(emailConfig(), "notes", "evil@[IPv6:2001:db8::1]"),
		"a plain phone number":  with(emailConfig(), "sms_backup", "5550100199"),
		"a 00 phone number":     with(emailConfig(), "sms_backup", "0044 20 7946 0000"),
		"a fullwidth at":        with(emailConfig(), "notes", "evil\uff20attacker.test"),
		"a percent-encoded at":  with(emailConfig(), "notes", "evil%40attacker.test"),
		"an address in a link":  with(emailConfig(), "callback", "https://x.test/hook?to=evil@attacker.test"),
		"a recipient last of @": with(emailConfig(), "notes", "ada@example.test@attacker.test"),
		// Only a string is prose: Graph's message object holds recipients.
		"an object under message": with(emailConfig(), "message", map[string]interface{}{"to": evil}),
		"an object under body":    with(emailConfig(), "body", map[string]interface{}{"address": evil}),
		"objects in content":      with(emailConfig(), "content", []interface{}{map[string]interface{}{"to": evil}}),
		// Copies by id: no address for the net to read, the key alone.
		"ccs with an id":      with(emailConfig(), "ccs", []interface{}{"5b10ac8d82e05b22cc7d4ef5"}),
		"bccs with an id":     with(emailConfig(), "bccs", "5b10ac8d82e05b22cc7d4ef6"),
		"watchers with an id": with(emailConfig(), "watchers", []interface{}{"5b10ac8d82e05b22cc7d4ef7"}),
		"fields.watcherIds":   with(emailConfig(), "fields", map[string]interface{}{"watcherIds": []interface{}{"5b10ac8d82e05b22cc7d4ef8"}}),
	} {
		if h, _ := held(t, st, "comm.email_send", cfg); !h {
			t.Errorf("%s: went out without a card", name)
		}
	}
	for name, cfg := range map[string]map[string]interface{}{
		"an address in the body":    with(emailConfig(), "body", "Write to "+evil+" too"),
		"an address in the subject": with(emailConfig(), "subject", "Re: "+evil),
		"the sender's login":        with(emailConfig(), "username", "me@example.test"),
		"a thread id":               with(emailConfig(), "in_reply_to", "<abc@mail.example.test>"),
		"strings in a list of text": with(emailConfig(), "content", []interface{}{"Write to " + evil}),
		"the recipient, escaped":    with(emailConfig(), "notes", "ada%40example.test and ADA\uff20example.test"),
		"a profile link":            with(emailConfig(), "link", "https://social.example.test/@ada"),
		"a date and an IP":          with(emailConfig(), "notes", "on 2026-10-19 from 10.0.0.1"),
	} {
		if h, _ := held(t, st, "comm.email_send", cfg); h {
			t.Errorf("%s: held, but it names no one else", name)
		}
	}
	// The recipient written with a display name names no one else either
	// (a rule is keyed on the address as written, so this is the net alone).
	if !soleRecipient(nil, target{to: []string{"to"}}, "comm.email_send", with(emailConfig(), "to", "Ada <ada@example.test>")) {
		t.Error("the recipient with a display name tripped the net")
	}
}

// Punctuation that ends a word next to an @ hides no address: quotes, a
// comment, brackets, braces, a pipe, a backslash, a backtick, a comma
// (security review of sole-net-widen, N1: each read as the handle
// @attacker.test). A quoted local part and a comment are valid addresses
// (RFC 5322), and ', |, {, } and ` are legal in a local part.
func TestAnAddressSplitAtItsAtGetsACard(t *testing.T) {
	st := allowFor(t, "comm.email_send", emailConfig())
	for _, a := range []string{"evil'@attacker.test", "evil|@attacker.test", "evil}@attacker.test", "evil{@attacker.test",
		"evil`@attacker.test", `"evil"@attacker.test`, `"ev il"@attacker.test`, "evil(c)@attacker.test", "evil[x]@attacker.test",
		`evil\@attacker.test`, "evil,@attacker.test", "evil;@attacker.test", "evil>@attacker.test", "evil@(c)attacker.test",
		"evil@{attacker.test}", "note: (evil)@attacker.test"} {
		if h, _ := held(t, st, "comm.email_send", with(emailConfig(), "notes", a)); !h {
			t.Errorf("%s: went out without a card", a)
		}
	}
	for _, a := range []string{`"Ada" <ada@example.test>`, "(ada@example.test)", "[ada@example.test]", "ping @ada", "<@U0123ABCD>"} {
		if h, _ := held(t, st, "comm.email_send", with(emailConfig(), "notes", a)); h {
			t.Errorf("%s: held, but it names no one else", a)
		}
	}
}

// An id's digits glued after a letter, and a Unix time under a time-like
// key, are no phone number (security review of sole-net-widen, N4); the
// same digits standing alone elsewhere still are.
func TestIdsAndTimesAreNoPhoneNumbers(t *testing.T) {
	st := allowFor(t, "comm.email_send", emailConfig())
	for name, cfg := range map[string]map[string]interface{}{
		"an incident number":         with(emailConfig(), "reference", "INC0012345"),
		"a SKU":                      with(emailConfig(), "reference", "ABC1234567"),
		"a tracking number":          with(emailConfig(), "reference", "1Z999AA10123456784"),
		"Arabic-Indic digits of one": with(emailConfig(), "reference", "INC\u0660\u0660\u0661\u0662\u0663\u0664\u0665"),
		"a Unix time in seconds":     with(emailConfig(), "send_at", "1760868000"),
		"a Unix time in ms":          with(emailConfig(), "options", map[string]interface{}{"createdAt": "1760868000123"}),
	} {
		if h, _ := held(t, st, "comm.email_send", cfg); h {
			t.Errorf("%s: held, but it names no one", name)
		}
	}
	for name, cfg := range map[string]map[string]interface{}{
		"a Unix time's digits under another key": with(emailConfig(), "reference", "1760868000"),
		"a phone number under a time key":        with(emailConfig(), "send_at", "5550100199"),
		"a phone number after an id":             with(emailConfig(), "reference", "INC0012345 5550100199"),
		"a phone number after a + or a colon":    with(emailConfig(), "reference", "tel:+15550100199"),
	} {
		if h, _ := held(t, st, "comm.email_send", cfg); !h {
			t.Errorf("%s: went out without a card", name)
		}
	}
}

// smtp_host names the server a send goes through: one other than the saved
// connection's is one more destination (it would take the connection's
// password too), and so is one with no saved connection behind it,
// whatever the config's credential map says, since whoever writes the
// config writes that map too (security review of #871 round 3). Either way
// the send gets its card whatever a rule says.
func TestAServerOtherThanTheConnectionsGetsACard(t *testing.T) {
	asAgent(t)
	db := commsDB(t)
	save := func(profile, host string) string {
		c := &connections.Connection{Platform: "smtp", Method: connections.MethodAppPass, ProfileID: profile,
			Data: map[string]interface{}{"smtp_host": host, "password": "hunter2"}}
		if err := connections.NewStore(db).Save(context.Background(), c); err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	own, othersProfile := save("p1", "smtp.example.test"), save("p2", "smtp.attacker.test")
	// A config as a run merges its connection in (mergeCredential).
	merged := func(id, host string) map[string]interface{} {
		return with(emailConfig(), "credential_id", id, "smtp_host", host,
			"credential", map[string]interface{}{"smtp_host": host, "password": "hunter2"})
	}
	st := allowIn(t, db, "comm.email_send", merged(own, "smtp.example.test"))
	for name, cfg := range map[string]map[string]interface{}{
		"the connection's server":         merged(own, "smtp.example.test"),
		"the connection's server in caps": with(merged(own, "smtp.example.test"), "smtp_host", "SMTP.example.test"),
	} {
		if h, _ := held(t, st, "comm.email_send", cfg); h {
			t.Errorf("%s: held", name)
		}
	}
	noID := merged("", "smtp.attacker.test")
	delete(noID, "credential_id")
	for name, cfg := range map[string]map[string]interface{}{
		"another host":                          with(merged(own, "smtp.example.test"), "smtp_host", "smtp.attacker.test"),
		"a credential map and no connection":    noID,
		"a credential_id that resolves to none": merged("nope", "smtp.attacker.test"),
		"another profile's connection":          merged(othersProfile, "smtp.attacker.test"),
		"its own host and no connection":        with(emailConfig(), "smtp_host", "smtp.example.test"),
		"no connection at all":                  emailConfig(),
	} {
		if h, _ := held(t, st, "comm.email_send", cfg); !h {
			t.Errorf("%s: went out without a card", name)
		}
	}
}

// An email rule is bound to the saved connection it was granted on: a send
// through another connection of the profile, on the same server or with
// no server in its config, sends as another mailbox with another password
// than the card's rule was for, so it gets its card (security review of
// sole-net-widen, N5).
func TestAnEmailRuleIsBoundToItsConnection(t *testing.T) {
	asAgent(t)
	db := commsDB(t)
	save := func(password string) string {
		c := &connections.Connection{Platform: "smtp", Method: connections.MethodAppPass, ProfileID: "p1",
			Data: map[string]interface{}{"smtp_host": "smtp.example.test", "password": password}}
		if err := connections.NewStore(db).Save(context.Background(), c); err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	merged := func(id string) map[string]interface{} {
		return with(emailConfig(), "credential_id", id, "smtp_host", "smtp.example.test",
			"credential", map[string]interface{}{"smtp_host": "smtp.example.test", "password": "x"})
	}
	own := save("hunter2")
	st := allowIn(t, db, "comm.email_send", merged(own))
	if rules, _ := st.Allows(context.Background()); len(rules) != 1 || rules[0].Account != "me@example.test via connection "+own {
		t.Errorf("rules %+v, want one on me@example.test via connection %s", rules, own)
	}
	for name, cfg := range map[string]map[string]interface{}{
		"its connection":                     merged(own),
		"its platform name, resolving to it": merged("smtp"),
	} {
		if h, _ := held(t, st, "comm.email_send", cfg); h {
			t.Errorf("%s: held", name)
		}
	}
	other := save("other-password")
	for name, cfg := range map[string]map[string]interface{}{
		"another connection, its server merged": merged(other),
		"another connection, no server named":   with(emailConfig(), "credential_id", other),
	} {
		if h, _ := held(t, st, "comm.email_send", cfg); !h {
			t.Errorf("%s: went out on the rule for %s", name, own)
		}
	}
}

// A rule is bound to the sending account a node names besides from:
// WhatsApp's phone_number_id and Graph's send-as mailbox. The business
// number's id reads as a phone number, and it is the sender's: the net
// reads past it.
func TestARuleIsBoundToTheSendingNumberAndMailbox(t *testing.T) {
	for _, c := range []struct {
		node, key, other string
		base             map[string]interface{}
	}{
		{"comm.whatsapp", "phone_number_id", "106540352242923", map[string]interface{}{"operation": "send_message", "phone_number_id": "106540352242922", "to": "+15550001", "message": "hi"}},
		{"service.outlook_mail", "mailbox", "boss@example.test", map[string]interface{}{"operation": "send_message", "access_token": "x", "mailbox": "me@example.test", "to": "ada@example.test", "body": "hi"}},
	} {
		st := allowFor(t, c.node, c.base)
		if h, _ := held(t, st, c.node, c.base); h {
			t.Errorf("%s: the send its rule names was held", c.node)
		}
		if h, p := held(t, st, c.node, with(c.base, c.key, c.other)); !h || p.Account.Handle != c.other {
			t.Errorf("%s from another %s: held=%v account %+v", c.node, c.key, h, p.Account)
		}
	}
}

// The card names the address a send goes to; the person People matched to
// it is a tag, and only for that whole address (third security review of
// #871: a LIKE match named Ada for da@example.test).
func TestTheCardNamesTheAddressAndTagsOnlyAnExactPerson(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	if _, err := db.Exec(`INSERT INTO people (id, platform_username, platform, full_name, contact_details) VALUES
		('p1', 'ada', 'EMAIL', 'Ada Lovelace', '{"email":"Ada@Example.test","phone":"+15550100"}'),
		('p2', 'ceo', 'EMAIL', 'The UK CEO', 'ceo@acme.co.uk')`); err != nil {
		t.Fatal(err)
	}
	st := Store{db, "p1"}
	for _, c := range []struct{ to, personID, personName string }{
		{"da@example.test", "", ""},
		{"a@exa", "", ""},
		{"ceo@acme.co", "", ""},
		{"ada@example.test", "p1", "Ada Lovelace"},
		{"Ada <ADA@example.test>", "p1", "Ada Lovelace"},
		{"ceo@acme.co.uk", "p2", "The UK CEO"},
	} {
		h, p := held(t, st, "comm.email_send", with(emailConfig(), "to", c.to))
		if !h || p.Recipient.Name != c.to || p.Recipient.PersonID != c.personID || p.Recipient.PersonName != c.personName {
			t.Errorf("to %q: held=%v recipient %+v, want the name %q and person %q (%q)", c.to, h, p.Recipient, c.to, c.personID, c.personName)
		}
	}
	_ = context.Background()
}
