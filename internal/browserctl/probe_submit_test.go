package browserctl

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/dop251/goja"
)

// probeDOM is just enough DOM for probeSubmitJS: elements with attributes,
// a parent chain, closest() for the two selector lists the probe uses, and
// forms whose elements getter lives on HTMLFormElement.prototype.
const probeDOM = `
class El {
  constructor(tag, attrs, props) {
    this.tagName = tag.toUpperCase(); this.nodeType = 1; this.attrs = attrs || {};
    this.parentElement = null; this.kids = []; this.innerText = "";
    this.id = this.attrs.id || ""; this.name = this.attrs.name || "";
    Object.assign(this, props || {});
  }
  getAttribute(k) { return k in this.attrs ? this.attrs[k] : null; }
  add(child) { child.parentElement = this; this.kids.push(child); return child; }
  get previousElementSibling() {
    const sibs = this.parentElement ? this.parentElement.kids : [];
    const i = sibs.indexOf(this);
    return i > 0 ? sibs[i - 1] : null;
  }
  matches(sel) {
    return sel.split(",").some((s) => {
      s = s.trim();
      if (s === "[role=button]") return this.attrs.role === "button";
      return this.tagName === s.toUpperCase();
    });
  }
  closest(sel) {
    for (let n = this; n; n = n.parentElement) if (n.matches(sel)) return n;
    return null;
  }
}
class HTMLFormElement extends El {}
Object.defineProperty(HTMLFormElement.prototype, "elements", { get() {
  const out = [];
  const walk = (n) => n.kids.forEach((k) => { if (["INPUT", "BUTTON", "SELECT", "TEXTAREA"].includes(k.tagName)) out.push(k); walk(k); });
  walk(this);
  return out;
}});
class URL {
  constructor(u, base) {
    this.href = /^https?:/.test(u) ? u : u.startsWith("/") ? base.match(/^https?:\/\/[^/]+/)[0] + u : base.replace(/[^/]*$/, "") + u;
  }
}
const CSS = { escape: (s) => s };
const location = { href: "https://example.com/compose/new" };
const html = new El("html");
const body = html.add(new El("body"));
const bySel = {};
const document = {
  documentElement: html,
  querySelector: (s) => bySel[s] || null,
  querySelectorAll: (s) => { const id = s.slice(1); const all = []; const walk = (n) => { if (n.id === id) all.push(n); n.kids.forEach(walk); }; walk(html); return all; },
};
const field = (form, tag, attrs, props) => { const f = form.add(new El(tag, attrs, props)); f.form = form; f.labels = []; return f; };

// A message form with a password, a hidden token, a card number, a submit
// button with a span inside, and a plain button.
const main = body.add(new El("main"));
main.add(new El("div"));
const form = main.add(new HTMLFormElement("form", { action: "/post", method: "POST" }));
// A field named "action" must not hide the form's own action.
field(form, "input", { name: "action" }, { type: "hidden", value: "send" });
field(form, "textarea", { name: "body" }, { type: "textarea", value: "hello" });
field(form, "input", { name: "pw" }, { type: "password", value: "hunter2" });
field(form, "input", { name: "card_number" }, { type: "text", value: "4111111111111111" });
field(form, "input", { name: "agree" }, { type: "checkbox", checked: true, value: "yes" });
field(form, "input", { name: "color" }, { type: "radio", checked: false, value: "red" });
const send = field(form, "button", { type: "submit", id: "send" }, { type: "submit" });
bySel["#send span"] = send.add(new El("span", {}, { innerText: "Send" }));
bySel["#send"] = send;
bySel["#toggle"] = field(form, "button", { type: "button", id: "toggle" }, { type: "button", innerText: "Show password" });
bySel["#menu"] = body.add(new El("button", { id: "menu" }, { innerText: "Open menu" }));
bySel["#publish"] = body.add(new El("a", { id: "publish" }, { innerText: "Publish now" }));
bySel["#big"] = body.add(new El("div", { id: "big" }, { innerText: "Publish ".repeat(20) }));
bySel["#sub"] = body.add(new El("input", { type: "submit", id: "sub" }, { type: "submit", value: "Go" }));
`

func runProbe(t *testing.T, selector string) SubmitTarget {
	t.Helper()
	vm := goja.New()
	if _, err := vm.RunString(probeDOM); err != nil {
		t.Fatalf("dom: %v", err)
	}
	sel, _ := json.Marshal(selector)
	v, err := vm.RunString(fmt.Sprintf(probeSubmitJS, string(sel)))
	if err != nil {
		t.Fatalf("probe %s: %v", selector, err)
	}
	raw, _ := json.Marshal(v.Export())
	var st SubmitTarget
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("probe %s answered %s: %v", selector, raw, err)
	}
	return st
}

func TestProbeSubmitClassifiesTheClickTarget(t *testing.T) {
	cases := []struct {
		sel    string
		submit bool
		why    string
	}{
		{"#send", true, "type=submit"},
		{"#send span", true, "type=submit"}, // a click inside the button is the button's
		{"#toggle", true, "a button inside a form"},
		{"#sub", true, "type=submit"},
		{"#publish", true, "its text"},
		{"#menu", false, ""},
		{"#big", false, ""}, // long text is not a label
	}
	for _, c := range cases {
		got := runProbe(t, c.sel)
		if !got.Found || got.Submit != c.submit || got.Why != c.why {
			t.Errorf("%s: %+v, want submit=%v why=%q", c.sel, got, c.submit, c.why)
		}
	}
	if got := runProbe(t, "#nothing"); got.Found {
		t.Errorf("missing element found: %+v", got)
	}
}

func TestProbeSubmitSummarizesTheFormWithSecretsMasked(t *testing.T) {
	got := runProbe(t, "#send")
	f := got.Form
	if f.Action != "https://example.com/post" || f.Method != "post" || f.Selector != "body:nth-of-type(1) > main:nth-of-type(1) > form:nth-of-type(1)" {
		t.Fatalf("form = %+v", f)
	}
	values := map[string]string{}
	for _, fl := range f.Fields {
		values[fl.Name] = fl.Value
	}
	want := map[string]string{"action": "•••", "body": "hello", "pw": "•••", "card_number": "•••", "agree": "yes"}
	for k, v := range want {
		if values[k] != v {
			t.Errorf("field %s = %q, want %q (all: %v)", k, values[k], v, values)
		}
	}
	if _, ok := values["color"]; ok {
		t.Error("an unchecked radio is listed")
	}
	if _, ok := values["send"]; ok {
		t.Error("the submit button is listed as a field")
	}
	if got := runProbe(t, "#menu"); got.Form.Selector != "" || len(got.Form.Fields) != 0 {
		t.Errorf("a control outside a form has a form: %+v", got.Form)
	}
}
