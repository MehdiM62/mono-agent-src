package workflow

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
)

// Where a saved connection's secret goes. A step that carries a
// connection's secret (credential_id, or `node run` finding the platform's
// connection) sends it to the hosts its config names: the SMTP relay, the
// Mastodon instance, the API's base URL, the broker. Those fields come from
// the connection when it has them, so a step can't point the connection's
// secret at a host of its own: a differing value is refused. Where the
// connection names none, the node's default is used, and a host the config
// sets must be written in a workflow the user approved, not taken from the
// run's data, nor set by an AI's step of its own (`node run`, MCP node_run).

// CredentialDestinations is implemented by an executor whose config fields
// decide where a saved connection's secret goes. Each group lists the keys
// that name one destination (the aliases the node reads, a URL and a bare
// host alike: only the host and port count). A key may be a path into a
// nested block with a credential_id of its own ("embedding.base_url"): the
// node compares it with that connection; here it must be written as it is.
type CredentialDestinations interface {
	CredentialDestinationKeys() [][]string
}

// ErrCredentialDestination is wrapped by every refusal of a destination.
var ErrCredentialDestination = errors.New("a saved connection's secret goes only to the host the connection or the user's own workflow names")

// CredentialUse is the saved connection a step's config carries: its data,
// filled into the config's unset keys, and what it is for.
type CredentialUse struct {
	Name     string // the connection's name, else its id or platform: for the errors
	Platform string // credential_platform: which service the connection is for
	Data     map[string]interface{}
}

// CheckCredentialDestinations refuses a step whose config sends use's
// secret elsewhere than use names, for executor's destination keys
// (CheckDestinationKeys). A nil use checks nothing.
func CheckCredentialDestinations(executor NodeExecutor, use *CredentialUse, approved bool, stored, resolved map[string]interface{}) error {
	d, ok := executor.(CredentialDestinations)
	if !ok {
		return nil
	}
	return CheckDestinationKeys(d.CredentialDestinationKeys(), use, approved, stored, resolved)
}

// CheckDestinationKeys refuses a config that sends use's secret elsewhere
// than use names. For each group of keys: where the connection names the
// destination (any key of the group; credential_platform from
// use.Platform), every key the config sets must name one of its hosts;
// where it names none, a key the config sets must be written in stored as
// it is (no expression in its host), and approved (a workflow the user
// approved: not AIAuthoredFrom, nor an AI's step of its own). stored is
// the config as written with the connection merged in, resolved the config
// after expressions.
func CheckDestinationKeys(keys [][]string, use *CredentialUse, approved bool, stored, resolved map[string]interface{}) error {
	for _, group := range keys {
		if err := checkDestinationGroup(group, use, approved, stored, resolved); err != nil {
			return err
		}
	}
	return nil
}

func checkDestinationGroup(group []string, use *CredentialUse, approved bool, stored, resolved map[string]interface{}) error {
	if len(group) > 0 && strings.Contains(group[0], ".") {
		return checkNestedDestinations(group, approved, stored, resolved)
	}
	if use == nil {
		return nil
	}
	data := use.Data
	if use.Platform != "" {
		data = make(map[string]interface{}, len(use.Data)+1)
		for k, v := range use.Data {
			data[k] = v
		}
		data["credential_platform"] = use.Platform
	}
	var theirs []string
	for _, k := range group {
		theirs = append(theirs, destinationsOf(data[k])...)
	}
	name := use.Name
	if name == "" {
		name = use.Platform
	}
	for _, k := range group {
		mine := destinationsOf(resolved[k])
		if len(mine) == 0 {
			continue
		}
		if len(theirs) > 0 {
			for _, m := range mine {
				if !namedBy(m, theirs) {
					return fmt.Errorf("%q: the saved connection %q sends its secret to %s, not %s: %w (change the connection to use another host)",
						k, name, strings.Join(theirs, ", "), m, ErrCredentialDestination)
				}
			}
			continue
		}
		if !approved {
			return fmt.Errorf("%q: %s isn't a host the saved connection %q names: in a workflow you haven't approved, or a step an AI runs on its own, %w",
				k, strings.Join(mine, ", "), name, ErrCredentialDestination)
		}
		if !writtenAsIs(stored[k], mine) {
			return fmt.Errorf("%q: the host the saved connection %q's secret goes to must be written in the workflow, not taken from its data: %w", k, name, ErrCredentialDestination)
		}
	}
	return nil
}

// checkNestedDestinations holds a nested block's destination keys
// ("embedding.base_url") to the rule for a host the connection doesn't
// name, when the block carries a credential_id: written as it is, in an
// approved workflow. The node compares them with that connection itself;
// a step that isn't approved is the node's to refuse.
func checkNestedDestinations(group []string, approved bool, stored, resolved map[string]interface{}) error {
	if !approved {
		return nil
	}
	for _, k := range group {
		block, key, _ := strings.Cut(k, ".")
		sb, _ := stored[block].(map[string]interface{})
		if id, _ := sb["credential_id"].(string); strings.TrimSpace(id) == "" {
			continue
		}
		rb, _ := resolved[block].(map[string]interface{})
		mine := destinationsOf(rb[key])
		if len(mine) > 0 && !writtenAsIs(sb[key], mine) {
			return fmt.Errorf("%q: the host the saved connection's secret goes to must be written in the workflow, not taken from its data: %w", k, ErrCredentialDestination)
		}
	}
	return nil
}

// writtenAsIs reports whether the stored value names the destinations
// mine, with no expression in them.
func writtenAsIs(stored interface{}, mine []string) bool {
	written := destinationsOf(stored)
	if len(written) != len(mine) {
		return false
	}
	for i := range written {
		if written[i] != mine[i] || strings.Contains(written[i], "{{") {
			return false
		}
	}
	return true
}

// namedBy reports whether destination d is one of theirs: the same host,
// and the same port when both name one.
func namedBy(d string, theirs []string) bool {
	host, port := splitDestination(d)
	for _, t := range theirs {
		th, tp := splitDestination(t)
		if th == host && (port == "" || tp == "" || port == tp) {
			return true
		}
	}
	return false
}

func splitDestination(d string) (host, port string) {
	if h, p, err := net.SplitHostPort(d); err == nil {
		return strings.Trim(h, "[]"), p
	}
	return strings.Trim(d, "[]"), ""
}

// destinationsOf is where a destination field's value sends a secret, in
// one form, sorted: the lowercase host[:port] of a URL (an http(s) one's
// port spelled out) or a bare host, without userinfo or path; a number as
// its digits; each host of a list of bare hosts ("a:9092,b:9092"); none
// for an empty value.
func destinationsOf(v interface{}) []string {
	var out []string
	switch t := v.(type) {
	case nil:
		return nil
	case []interface{}:
		for _, x := range t {
			out = append(out, destinationsOf(x)...)
		}
	case []string:
		for _, x := range t {
			out = append(out, destinationsOf(x)...)
		}
	case string:
		parts := []string{t}
		if !strings.Contains(t, "://") {
			parts = strings.Split(t, ",") // a list of hosts ("a:9092,b:9092")
		}
		for _, part := range parts {
			if d := destinationOf(part); d != "" {
				out = append(out, d)
			}
		}
	default:
		if d := destinationOf(fmt.Sprint(t)); d != "" {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

func destinationOf(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	scheme := ""
	if i := strings.Index(s, "://"); i >= 0 {
		scheme, s = s[:i], s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#\\"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(s, ".")
	if _, port := splitDestination(s); port == "" && s != "" {
		switch scheme {
		case "https":
			s += ":443"
		case "http":
			s += ":80"
		}
	}
	return s
}
