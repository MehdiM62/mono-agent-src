package hilsend

import (
	"context"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/monoes/mono-agent/internal/commsync"
)

// Every send item also has a row in Communications (person_messages), made
// when it is queued, that the status changes below keep up to date. See
// internal/commsync. A failure here never blocks or fails the send itself; it
// is logged.

func warn(err error, what string) {
	if err != nil {
		log.Warn().Err(err).Msg("hilsend: communications: " + what)
	}
}

// commsItem maps a send card to the Communications item.
func commsItem(profile string, p Payload, rp replay) commsync.Item {
	it := commsync.Item{
		HilRef: p.ID, Profile: profile, Source: p.Channel, Platform: p.Account.Platform,
		Action: p.Action, Account: commsync.Account{Platform: p.Account.Platform, Handle: p.Account.Handle},
		Subject: p.Subject, Text: p.Text, TextField: rp.TextField, SubjectField: rp.SubjectKey,
		Meta: map[string]interface{}{"origin": "send", "node_type": p.NodeType, "prepared_by": p.PreparedBy},
	}
	if len(p.Attachments) > 0 {
		it.Attachments = p.Attachments
	}
	if p.Action == ActionEmail || p.Channel == "outlook" || p.Channel == "gmail" {
		it.Platform = "email"
	}
	// A post, publish or channel message is addressed to nobody: it is linked
	// to our own account. The destination stays in the metadata. So is a
	// send to a label ("the ticket's requester"), which is nobody's People row.
	if p.Action == ActionPost || p.Action == ActionPublish || p.Recipient.Label {
		it.Meta["target"] = p.Recipient
		return it
	}
	it.Recipient = commsync.Recipient{PersonID: p.Recipient.PersonID, Name: p.Recipient.Name, ProfileURL: p.Recipient.ProfileURL}
	if p.Action == ActionEmail {
		it.Recipient.Handle = strings.ToLower(strings.TrimSpace(p.Recipient.Name))
	}
	return it
}

func (s Store) commsQueue(ctx context.Context, p Payload, rp replay) {
	_, err := commsync.Queue(ctx, s.DB, commsItem(s.Profile, p, rp))
	warn(err, "queue")
}

func (s Store) commsStatus(ctx context.Context, id string, to commsync.Status, d commsync.Detail) {
	warn(commsync.SetStatus(ctx, s.DB, id, to, d), "status "+string(to))
}

// WorkflowItems returns the Communications item of every input item of a
// Human-in-Loop node, given the nodes after it: one entry per input item, nil
// where the item is not an outbound send. The node types are classified the way
// agent sends are, so the same channels, accounts and recipients result.
func WorkflowItems(profile, workflowName string, downstream []commsync.Node, items []map[string]interface{}) []*commsync.Item {
	out := make([]*commsync.Item, len(items))
	var sends []commsync.Node
	var t target
	for _, n := range downstream {
		if tt, ok := classify(n.Type, n.Config); ok {
			if len(sends) == 0 {
				t = tt
			}
			sends = append(sends, n)
		}
	}
	if len(sends) == 0 {
		return out
	}
	first := sends[0]
	targets := make([]string, 0, len(sends))
	for _, n := range sends {
		targets = append(targets, n.ID)
	}
	for i, item := range items {
		cfg := cloneMap(first.Config)
		textField, text := firstKey(cfg, t.text)
		text = fromTemplate(text, item, t.text)
		subjectField, subject := firstKey(cfg, t.subject)
		subject = fromTemplate(subject, item, t.subject)
		p := Payload{
			Action: t.action, Channel: t.channelOf(first.Type), Account: t.accountOf(first.Type, cfg),
			Text: text, Subject: subject, NodeType: first.Type, Attachments: attachments(cfg),
		}
		rp := replay{TextField: textField, SubjectKey: subjectField}
		it := commsItem(profile, p, rp)
		it.Meta["origin"] = "workflow"
		it.Meta["workflow"] = map[string]interface{}{
			"name": workflowName, "target_nodes": targets, "target_node_type": first.Type,
		}
		it.Recipient = workflowRecipient(t, first.Type, cfg, item, p.Action)
		if _, labelled := t.recipientOf(cfg); labelled || p.Action == ActionPost || p.Action == ActionPublish {
			it.Recipient = commsync.Recipient{}
		}
		// The field of the HIL item that holds the text, when the node's own
		// text field is a template over it, is what an edit changes.
		it.TextField = editField(cfg, textField, item, t.text)
		out[i] = &it
	}
	return out
}

var tmplField = regexp.MustCompile(`\$json(?:\.([A-Za-z0-9_]+)|\[['"]([^'"]+)['"]\])`)

// templateKey returns the item field a "{{ $json.x }}" value reads, or "".
func templateKey(v string) string {
	if !strings.Contains(v, "{{") {
		return ""
	}
	m := tmplField.FindStringSubmatch(v)
	if m == nil {
		return ""
	}
	return firstNonEmpty(m[1], m[2])
}

// fromTemplate resolves a config value that reads a field of the item. A
// literal value is returned as is; when the config has nothing, the item's own
// text fields are used.
func fromTemplate(v string, item map[string]interface{}, keys []string) string {
	if k := templateKey(v); k != "" {
		if s := str(item[k]); s != "" {
			return s
		}
		return ""
	}
	if v != "" {
		return v
	}
	for _, k := range keys {
		if s := str(item[k]); s != "" {
			return s
		}
	}
	return ""
}

// editField is the key an operator's edit of the item's text lands in: the
// item field the node's text template reads, else a text field the item has.
func editField(cfg map[string]interface{}, textField string, item map[string]interface{}, keys []string) string {
	if k := templateKey(str(valueAt(cfg, textField))); k != "" {
		return k
	}
	for _, k := range keys {
		if str(item[k]) != "" {
			return k
		}
	}
	return textField
}

// workflowRecipient reads who an item is addressed to from the item (and, for
// an email, from the node's "to").
func workflowRecipient(t target, nodeType string, cfg, item map[string]interface{}, action string) commsync.Recipient {
	r := commsync.Recipient{}
	if action == ActionEmail {
		to := fromTemplate(firstValue(cfg, t.to), item, []string{"email", "to", "address"})
		if l := list(to); len(l) > 0 {
			to = l[0]
		}
		r.Handle, r.Name = strings.ToLower(to), to
		if n := firstStr(item, "name", "fullName", "full_name"); n != "" {
			r.Name = n
		}
		return r
	}
	u := firstStr(item, "profileUrl", "profile_url", "authorUrl", "author_url", "companyUrl", "company_url")
	if u == "" && (action == ActionDM || action == ActionConnect) {
		u = firstStr(item, "url", "href", "link")
	}
	r.ProfileURL = u
	r.Handle = strings.TrimPrefix(firstStr(item, "username", "handle", "author", "screen_name"), "@")
	r.Name = firstStr(item, "name", "fullName", "full_name", "company", "companyName", "company_name")
	if r.Name == "" {
		r.Name = r.Handle
	}
	if strings.ToLower(firstStr(item, "kind", "type")) == commsync.KindCompany {
		r.Kind = commsync.KindCompany
	}
	if r.Handle == "" && r.ProfileURL == "" && r.Name == "" {
		return commsync.Recipient{}
	}
	return r
}

func firstValue(m map[string]interface{}, keys []string) string {
	_, v := firstKey(m, append(append([]string{}, keys...), toKeys...))
	return v
}
