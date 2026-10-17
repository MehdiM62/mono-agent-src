package apipkg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Sends is what an operation that sends a message to people, or posts one
// in public, tells internal/hilsend: run by an agent or by a workflow the
// user never approved, the send waits for the user's approval (an approval
// card) instead of going out. "sends": false marks an operation that only
// looks like a send (a queue, a webhook, a lookup), with sends_reason
// saying why. An operation without either sends as before.
type Sends struct {
	// None is "sends": false.
	None bool `json:"-"`
	// Action is what it does to people: email, dm, post, comment, publish,
	// connect or invite.
	Action string `json:"action"`
	// To, Text and Subject name the op's params that hold the recipient,
	// the message and its subject for the approval card (the first that is
	// set wins).
	To      []string `json:"to,omitempty"`
	Text    []string `json:"text,omitempty"`
	Subject []string `json:"subject,omitempty"`
	// If names a param the op only sends with, when it is true (Sendy
	// creates a campaign and sends it only with sendCampaign).
	If string `json:"if,omitempty"`
}

// sendActions are the actions an approval card knows (internal/hilsend).
var sendActions = map[string]bool{"email": true, "dm": true, "post": true, "comment": true, "publish": true, "connect": true, "invite": true}

// UnmarshalJSON reads false or the object.
func (s *Sends) UnmarshalJSON(b []byte) error {
	if string(bytes.TrimSpace(b)) == "false" {
		*s = Sends{None: true}
		return nil
	}
	type plain Sends
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return fmt.Errorf("sends is false or {action, to, text, subject, if}: %w", err)
	}
	*s = Sends(p)
	return nil
}

// MarshalJSON writes false for None.
func (s Sends) MarshalJSON() ([]byte, error) {
	if s.None {
		return []byte("false"), nil
	}
	type plain Sends
	return json.Marshal(plain(s))
}

func (o *Operation) checkSends(where string, fields map[string]bool) []string {
	var probs []string
	add := func(format string, a ...any) { probs = append(probs, where+": "+fmt.Sprintf(format, a...)) }
	s := o.Sends
	switch {
	case s == nil:
		if o.SendsReason != "" {
			add("sends_reason without \"sends\": false")
		}
		return probs
	case s.None:
		if strings.TrimSpace(o.SendsReason) == "" {
			add("\"sends\": false needs a sends_reason")
		}
		return probs
	}
	if o.SendsReason != "" {
		add("sends_reason goes with \"sends\": false only")
	}
	if !sendActions[s.Action] {
		add("sends.action %q must be email, dm, post, comment, publish, connect or invite", s.Action)
	}
	for _, keys := range [][]string{s.To, s.Text, s.Subject, {s.If}} {
		for _, k := range keys {
			if k != "" && !fields[k] {
				add("sends names %q, which is not one of the op's fields", k)
			}
		}
	}
	return probs
}

// Send is a send SendOf found: the op's annotation and where its message
// goes, for internal/hilsend.
type Send struct {
	Action            string
	Channel           string // the app, for the card; "" for a native node's extension
	To, Text, Subject []string
	// PerItem lists the params the node resolves per input item (an app
	// node's fields); a native node's extension resolves none.
	PerItem []string
}

// SendOf reports whether running nodeType with config would send, as the
// op that config selects is annotated: an app node's package op (its
// default op when config names none), or the op a package adds to a native
// node. An op without a sends annotation, or one the config doesn't select,
// is not one.
func SendOf(nodeType string, config map[string]any) (Send, bool) {
	if pkg, ok := ForNodeType(nodeType); ok {
		op, err := pkg.resolveOp(config)
		if err != nil || !op.sends(config) {
			return Send{}, false
		}
		s := op.Sends
		perItem := pkg.fieldKeys()
		if op.Batch {
			perItem = nil // one request for every item: one card
		}
		return Send{Action: s.Action, Channel: pkg.ID, To: s.To, Text: s.Text, Subject: s.Subject, PerItem: perItem}, true
	}
	if pkg := extensionFor(nodeType); pkg != nil {
		opName := strings.TrimSpace(str(config["operation"]))
		if opName == "" {
			return Send{}, false
		}
		op, ok := pkg.Op(strings.TrimSpace(str(config["resource"])), opName)
		if !ok || !op.sends(config) {
			return Send{}, false
		}
		s := op.Sends
		return Send{Action: s.Action, To: s.To, Text: s.Text, Subject: s.Subject}, true
	}
	return Send{}, false
}

// SendsSome reports whether some op of nodeType's package, or of the
// package extending it, sends.
func SendsSome(nodeType string) bool {
	pkg, ok := ForNodeType(nodeType)
	if !ok {
		pkg = extensionFor(nodeType)
	}
	if pkg == nil {
		return false
	}
	for i := range pkg.Operations {
		if s := pkg.Operations[i].Sends; s != nil && !s.None {
			return true
		}
	}
	return false
}

// sends reports whether the op, run with config, sends: it is annotated,
// and its If param (when it names one) is true.
func (o *Operation) sends(config map[string]any) bool {
	s := o.Sends
	if s == nil || s.None {
		return false
	}
	if s.If == "" {
		return true
	}
	switch v := config[s.If].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	}
	return false
}
