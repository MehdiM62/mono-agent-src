package apipkg

import (
	"encoding/json"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
)

// A request merges some collection params whole ("$merge": "{{
// p.updateFields }}"), so a key the collection doesn't declare goes out as
// is: a raw "article" under a Zammad ticket's updateFields, a "bcc_emails"
// under Freshservice's additionalFields. The send gate can't judge such a
// key by the op's sends annotation, so SendOf takes a config with one as a
// send: an agent's or an unapproved workflow's run waits for approval, and
// everyone else's runs as before (the request is what it always was).
// Raw JSON goes through the params an op declares for it (jsonParameters,
// additionalFieldsJson), which its annotation judges.

// wholeUse finds a param a template uses whole: p.updateFields or
// p.article.articleDetails, followed by a filter or the template's end.
var wholeUse = regexp.MustCompile(`p\.(\w+)(\.\w+)?\s*(?:\||\}\})`)

var wholeCache sync.Map // *Operation → map[string]bool

// usedWhole is the param paths ("updateFields", "article.articleDetails")
// o's requests use whole.
func (o *Operation) usedWhole() map[string]bool {
	if v, ok := wholeCache.Load(o); ok {
		return v.(map[string]bool)
	}
	b, _ := json.Marshal([]any{o.Path, o.Query, o.Body, o.Headers, o.Before, o.Also, o.Bulk})
	out := map[string]bool{}
	for _, m := range wholeUse.FindAllStringSubmatch(string(b), -1) {
		out[m[1]+m[2]] = true
	}
	wholeCache.Store(o, out)
	return out
}

// undeclared lists what config sends that o doesn't declare: a key of a
// collection its requests use whole that isn't one of the collection's
// options, a key of an option object that isn't one of its subfields, or
// such a collection or option given as an expression (its keys are the
// run's).
func (o *Operation) undeclared(config map[string]any) []string {
	whole := o.usedWhole()
	var out []string
	for _, f := range o.Fields {
		if f.Type != "collection" && f.Type != "fixedCollection" {
			continue
		}
		v := config[f.Key]
		if s, ok := v.(string); ok && hasTemplate(s) {
			if whole[f.Key] || o.hasSubfields(f.Key) {
				out = append(out, f.Key+" (an expression)")
			}
			continue
		}
		c, ok := v.(map[string]any)
		if !ok {
			continue
		}
		for k, ov := range c {
			if whole[f.Key] && len(f.Options) > 0 && !slices.Contains(f.Options, k) {
				out = append(out, k)
				continue
			}
			sub, ok := o.Subfields[f.Key+"."+k]
			if !ok || !whole[f.Key] && !whole[f.Key+"."+k] {
				continue
			}
			if s, ok := ov.(string); ok && hasTemplate(s) {
				out = append(out, k+" (an expression)")
				continue
			}
			for _, obj := range objectsOf(ov) {
				for sk := range obj {
					if !slices.Contains(sub, sk) {
						out = append(out, k+"."+sk)
					}
				}
			}
		}
	}
	sort.Strings(out)
	return slices.Compact(out)
}

// hasSubfields reports an option of collection coll with declared subfields.
func (o *Operation) hasSubfields(coll string) bool {
	for k := range o.Subfields {
		if strings.HasPrefix(k, coll+".") {
			return true
		}
	}
	return false
}

// undeclaredSend is the send SendOf reports for keys o doesn't declare:
// an email, as nothing says what they do, to a recipient the card names
// by those keys.
func (o *Operation) undeclaredSend(keys []string) *Sends {
	return &Sends{Action: "email", ToUnset: "whoever its undeclared fields (" + strings.Join(keys, ", ") + ") reach"}
}

// objectsOf is v's objects: v itself, or the objects in a list.
func objectsOf(v any) []map[string]any {
	switch x := v.(type) {
	case map[string]any:
		return []map[string]any{x}
	case []any:
		var out []map[string]any
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// Undeclared lists what running nodeType with config would send that its
// op doesn't declare (see undeclared): for an app node, or a native node
// a package extends; nil for any other node type.
func Undeclared(nodeType string, config map[string]any) []string {
	if pkg, ok := ForNodeType(nodeType); ok {
		op, err := pkg.resolveOp(config)
		if err != nil {
			return nil
		}
		return op.undeclared(config)
	}
	if pkg := extensionFor(nodeType); pkg != nil {
		if op, ok := pkg.Op(strings.TrimSpace(str(config["resource"])), strings.TrimSpace(str(config["operation"]))); ok {
			return op.undeclared(config)
		}
	}
	return nil
}
