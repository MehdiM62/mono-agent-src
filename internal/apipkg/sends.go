package apipkg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
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
	// set wins). A dotted name ("updateFields.publicReply") is an option of
	// a collection param.
	To      []string `json:"to,omitempty"`
	Text    []string `json:"text,omitempty"`
	Subject []string `json:"subject,omitempty"`
	// Show names the params the card lists, read-only, in place of a
	// message, for an op whose words are settings or a flow's inputs rather
	// than a message the approver could edit (Zulip's stream update, a
	// Salesforce flow's variables). It goes without Text. "param if cond"
	// (cond as in If) marks the param "not sent" on the card when cond
	// doesn't hold: "variablesJson if jsonParameters".
	Show []string `json:"show,omitempty"`
	// ToLabel names what a To param holds when it is not an address the
	// card can show as it is ("invoiceId": "invoice" shows "invoice inv9");
	// ToUnset is the card's recipient when no To param is set ("the
	// appointment's attendees").
	ToLabel map[string]string `json:"to_label,omitempty"`
	ToUnset string            `json:"to_unset,omitempty"`
	// If names the params the op only sends with: it sends when one of them
	// is set (true, or any value but false, "" or an empty list), a param's
	// field default included; a list on a param's path is set when one of
	// its elements is, and has a value when one of them does.
	// "param=value" sends when the param has that value, "param!=value"
	// when it is unset or has another, "param not in a|b|c" when it is
	// set to none of those, and "param?" when it is there at all, "" and
	// false included. Sendy creates a campaign and sends it only with
	// sendCampaign; a Google Calendar event invites only with
	// additionalFields.attendees; Home Assistant notifies only with
	// "domain=notify"; Onfleet texts a task's recipient unless
	// recipientSkipSMSNotifications is true; a Zammad article goes out
	// unless it is a note, web or phone one; Zulip's bot posts a stream's
	// description even when it is cleared to "".
	If Cond `json:"if,omitempty"`
}

// Cond is "if": a param, or a list of them.
type Cond []string

// UnmarshalJSON reads a string or a list of strings.
func (c *Cond) UnmarshalJSON(b []byte) error {
	var one string
	if json.Unmarshal(b, &one) == nil {
		*c = Cond{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("sends.if is a param or a list of them: %w", err)
	}
	*c = many
	return nil
}

// MarshalJSON writes one param as a string.
func (c Cond) MarshalJSON() ([]byte, error) {
	if len(c) == 1 {
		return json.Marshal(c[0])
	}
	return json.Marshal([]string(c))
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
		return fmt.Errorf("sends is false or {action, to, text, subject, to_label, to_unset, if}: %w", err)
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
	if len(s.Show) > 0 && len(s.Text) > 0 {
		add("sends.show and sends.text don't go together: the card lists the params or shows a message")
	}
	conds := append([]string{}, s.If...)
	names := append(append(append([]string{}, s.To...), s.Text...), s.Subject...)
	for _, k := range s.Show {
		name, cond, ok := strings.Cut(k, " if ")
		names = append(names, name)
		if ok {
			conds = append(conds, cond)
		}
	}
	for _, k := range conds {
		// "param?" alone: on "p=x?", "p not in a|b?" or "p?!" the "?" would
		// be read as part of a value, and the condition would never hold.
		if strings.Contains(k, "?") && (strings.Count(k, "?") > 1 || !strings.HasSuffix(k, "?") ||
			strings.ContainsAny(k, "=!") || strings.Contains(k, notIn)) {
			add("sends.if %q: \"?\" goes alone at the end of a param (\"param?\"), not with =, != or not in", k)
		}
	}
	for i, keys := range [][]string{names, conds} {
		for _, k := range keys {
			if i == 0 && strings.Contains(k, "?") {
				add("sends names %q: \"?\" goes in sends.if only", k)
				continue
			}
			name, _, _ := strings.Cut(k, notIn)
			name, _, _ = strings.Cut(name, "=")
			name = strings.TrimSuffix(strings.TrimSuffix(name, "!"), "?")
			if head, _, _ := strings.Cut(name, "."); name == "" || !fields[head] {
				add("sends names %q, which is not one of the op's fields", k)
			}
		}
	}
	for k := range s.ToLabel {
		if !slices.Contains(s.To, k) {
			add("sends.to_label names %q, which is not in sends.to", k)
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
	ToLabel           map[string]string
	ToUnset           string
	// Op and Show are set for a send of keys the op doesn't declare
	// (Undeclared) and for one whose annotation shows params (Sends.Show):
	// the op ("trello card/update") and the param paths the card lists as
	// its text, read-only, in place of a message. Exprs are the Show paths
	// given as an expression, which the card says in place of a value.
	Op         string
	Show       []string
	Exprs      []string
	Undeclared bool
	// Unsent are the Show params the run doesn't send ("param if cond"
	// whose cond doesn't hold), which the card marks as such.
	Unsent []string
	// PerItem lists the params the node resolves per input item (an app
	// node's fields); a native node's extension resolves none.
	PerItem []string
}

// SendOf reports whether running nodeType with config would send, as the
// op that config selects is annotated: an app node's package op (its
// default op when config names none), or the op a package adds to a native
// node. A config with a key the op doesn't declare in a collection its
// request merges whole (collections.go) may send too, whatever the op's
// annotation. Anything else is not a send.
func SendOf(nodeType string, config map[string]any) (Send, bool) {
	if pkg, ok := ForNodeType(nodeType); ok {
		op, err := pkg.resolveOp(config)
		if err != nil {
			return Send{}, false
		}
		s, ok := op.sendFor(pkg, config)
		if !ok {
			return Send{}, false
		}
		s.Channel, s.PerItem = pkg.ID, pkg.fieldKeys()
		if op.Batch {
			s.PerItem = nil // one request for every item: one card
		}
		return s, true
	}
	if pkg := extensionFor(nodeType); pkg != nil {
		opName := strings.TrimSpace(str(config["operation"]))
		if opName == "" {
			return Send{}, false
		}
		op, ok := pkg.Op(strings.TrimSpace(str(config["resource"])), opName)
		if !ok {
			return Send{}, false
		}
		return op.sendFor(pkg, config)
	}
	return Send{}, false
}

// sendFor is the send o of pkg makes with config: as annotated, or, when
// that doesn't send, one of keys o doesn't declare, if config has some.
// Either may list params on the card in place of a message (Show).
func (o *Operation) sendFor(pkg *Package, config map[string]any) (Send, bool) {
	s := o.Sends
	var out Send
	switch {
	case !o.sends(config):
		keys := o.undeclared(config)
		if len(keys) == 0 {
			return Send{}, false
		}
		s = o.undeclaredSend(keys)
		out.Op, out.Undeclared = o.opLabel(pkg), true
		out.Show, out.Exprs = undeclaredPaths(keys)
	case len(s.Show) > 0:
		out.Op = o.opLabel(pkg)
		params := o.runParams(config)
		for _, e := range s.Show {
			p, cond, _ := strings.Cut(e, " if ")
			out.Show = append(out.Show, p)
			if cond != "" && !condHolds(params, cond) {
				out.Unsent = append(out.Unsent, p)
			}
			if v, ok := paramAt(config, p).(string); ok && hasTemplate(v) {
				out.Exprs = append(out.Exprs, p)
			}
		}
	}
	out.Action, out.To, out.Text, out.Subject, out.ToLabel, out.ToUnset = s.Action, s.To, s.Text, s.Subject, s.ToLabel, s.ToUnset
	return out, true
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

// notIn separates "param not in a|b|c": the param is set, to none of a, b, c.
const notIn = " not in "

// sends reports whether the op, run with config, sends: it is annotated,
// and one of its If params (when it names some) is set, has the value
// "param=value" names, is unset or has another ("param!=value"), is set
// to none of "param not in a|b|c", or is there at all ("param?"), as the
// run sees it: unset params take their field default. A template the run
// has yet to resolve counts as set, and as any value.
func (o *Operation) sends(config map[string]any) bool {
	s := o.Sends
	if s == nil || s.None {
		return false
	}
	if len(s.If) == 0 {
		return true
	}
	params := o.runParams(config)
	return slices.ContainsFunc(s.If, func(k string) bool { return condHolds(params, k) })
}

// runParams is config as the run sees it: unset params take their field
// default.
func (o *Operation) runParams(config map[string]any) map[string]any {
	params := make(map[string]any, len(config))
	for k, v := range config {
		params[k] = v
	}
	applyDefaults(o, params)
	return params
}

// condHolds reports whether one sends.if condition holds for params.
func condHolds(params map[string]any, k string) bool {
	if name, ok := strings.CutSuffix(k, "?"); ok {
		return slices.ContainsFunc(condValues(params, name), func(v any) bool { return v != nil })
	}
	if name, set, ok := strings.Cut(k, notIn); ok {
		v := paramAt(params, name)
		got := strings.TrimSpace(fmt.Sprint(v))
		return isSet(v) && (hasTemplate(got) || !slices.ContainsFunc(strings.Split(set, "|"), func(w string) bool {
			return strings.EqualFold(strings.TrimSpace(w), got)
		}))
	}
	name, want, eq := strings.Cut(k, "=")
	not := strings.HasSuffix(name, "!")
	v := paramAt(params, strings.TrimSuffix(name, "!"))
	got := strings.TrimSpace(fmt.Sprint(v))
	switch {
	case !eq:
		return slices.ContainsFunc(condValues(params, name), isSet)
	case v != nil && hasTemplate(got):
		return true
	case not:
		return v == nil || !strings.EqualFold(got, want)
	}
	return slices.ContainsFunc(condValues(params, name), func(x any) bool {
		got := strings.TrimSpace(fmt.Sprint(x))
		return x != nil && (hasTemplate(got) || strings.EqualFold(got, want))
	})
}

// condValues is the values at a bare condition's param name. A list on the
// way fans out to its elements: "itemsUi.itemsValues.note" is every
// item's note, and is set when one of them is.
func condValues(params map[string]any, name string) []any {
	if v, ok := params[name]; ok || !strings.Contains(name, ".") {
		return []any{v}
	}
	var walk func(v any, path []string) []any
	walk = func(v any, path []string) []any {
		if len(path) == 0 {
			return []any{v}
		}
		switch c := v.(type) {
		case map[string]any:
			return walk(c[path[0]], path[1:])
		case []any:
			var out []any
			for _, e := range c {
				out = append(out, walk(e, path)...)
			}
			return out
		}
		return nil
	}
	return walk(params, strings.Split(name, "."))
}

func isSet(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		x = strings.TrimSpace(x)
		return x != "" && !strings.EqualFold(x, "false")
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}
