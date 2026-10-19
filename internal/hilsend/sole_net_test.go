package hilsend

import (
	"context"
	"slices"
	"testing"
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
	for _, k := range []string{"ccEmail", "bccEmail", "bccAddress", "bcc_address", "Bcc", "CC", "ccRecipients", "additional_emails", "attendees", "recipients"} {
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
	} {
		if h, _ := held(t, st, "comm.email_send", cfg); h {
			t.Errorf("%s: held, but it names no one else", name)
		}
	}
	// The recipient written with a display name names no one else either
	// (a rule is keyed on the address as written, so this is the net alone).
	if !soleRecipient(target{to: []string{"to"}}, "comm.email_send", with(emailConfig(), "to", "Ada <ada@example.test>")) {
		t.Error("the recipient with a display name tripped the net")
	}
}

// smtp_host names the server a send goes through: one other than the saved
// connection's, or one set with no connection to compare, is one more
// destination (it would take the connection's password too), so the send
// gets its card whatever a rule says.
func TestAServerOtherThanTheConnectionsGetsACard(t *testing.T) {
	st := allowFor(t, "comm.email_send", emailConfig())
	noConnection := emailConfig()
	delete(noConnection, "credential")
	for name, cfg := range map[string]map[string]interface{}{
		"another host":           with(emailConfig(), "smtp_host", "smtp.attacker.test"),
		"no connection to match": noConnection,
	} {
		if h, _ := held(t, st, "comm.email_send", cfg); !h {
			t.Errorf("%s: went out without a card", name)
		}
	}
	if h, _ := held(t, st, "comm.email_send", with(emailConfig(), "smtp_host", "SMTP.example.test")); h {
		t.Error("the connection's own server, in another case, was held")
	}
}

// A rule is bound to the sending account a node names besides from:
// WhatsApp's phone_number_id and Graph's send-as mailbox.
func TestARuleIsBoundToTheSendingNumberAndMailbox(t *testing.T) {
	for _, c := range []struct {
		node, key, other string
		base             map[string]interface{}
	}{
		{"comm.whatsapp", "phone_number_id", "p2", map[string]interface{}{"operation": "send_message", "phone_number_id": "p1", "to": "+15550001", "message": "hi"}},
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
