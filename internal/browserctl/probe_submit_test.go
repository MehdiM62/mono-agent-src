package browserctl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// probeDOM is just enough DOM for the page scripts: elements with
// attributes, a parent chain, closest() and querySelectorAll() for the
// selector lists the scripts use, forms whose elements getter lives on
// HTMLFormElement.prototype, FormData, and click()/requestSubmit() that
// record what they did.
const probeDOM = `
const clicks = [], submits = [];
const FIELD_TAGS = ["INPUT", "BUTTON", "SELECT", "TEXTAREA"];
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
      if (s === "[contenteditable]") return "contenteditable" in this.attrs;
      return this.tagName === s.toUpperCase();
    });
  }
  closest(sel) {
    for (let n = this; n; n = n.parentElement) if (n.matches(sel)) return n;
    return null;
  }
  querySelectorAll(sel) {
    const out = [];
    const walk = (n) => n.kids.forEach((k) => { if (k.matches(sel)) out.push(k); walk(k); });
    walk(this);
    return out;
  }
  querySelector(sel) { return this.querySelectorAll(sel)[0] || null; }
  click() { clicks.push(this.id || this.tagName); }
  compareDocumentPosition(other) {
    const order = [];
    const walk = (n) => { order.push(n); n.kids.forEach(walk); };
    walk(html);
    return order.indexOf(other) > order.indexOf(this) ? 4 : 2;
  }
}
class HTMLFormElement extends El {
  requestSubmit() { submits.push(this.id || "form"); }
}
Object.defineProperty(HTMLFormElement.prototype, "elements", { get() {
  return this.querySelectorAll("input,button,select,textarea");
}});
class FormData {
  constructor(form, submitter) {
    if (submitter && submitter.form !== form) throw new TypeError("not a submitter of this form");
    this.list = [];
    for (const f of form.querySelectorAll("input,button,select,textarea")) {
      if (!f.name || f.disabled) continue;
      const t = (f.type || "").toLowerCase();
      if (["submit", "button", "reset", "image"].includes(t)) { if (f === submitter) this.list.push([f.name, f.value || ""]); continue; }
      if ((t === "checkbox" || t === "radio") && !f.checked) continue;
      if (t === "file") { for (const file of f.files || []) this.list.push([f.name, file]); continue; }
      this.list.push([f.name, String(f.value == null ? "" : f.value)]);
    }
  }
  entries() { return this.list[Symbol.iterator](); }
}
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
  documentElement: html, body,
  querySelector: (s) => bySel[s] || null,
  querySelectorAll: (s) => { const id = s.slice(1); const all = []; const walk = (n) => { if (n.id === id) all.push(n); n.kids.forEach(walk); }; walk(html); return all; },
};
const field = (form, tag, attrs, props) => { const f = form.add(new El(tag, attrs, props)); f.form = form; f.labels = []; return f; };

// A message form with a password, a hidden token, a card number, a submit
// button with a span inside, a plain button, and many more fields.
const main = body.add(new El("main"));
main.add(new El("div"));
const form = main.add(new HTMLFormElement("form", { action: "/post", method: "POST", id: "login" }));
// A field named "action" must not hide the form's own action.
field(form, "input", { name: "action" }, { type: "hidden", value: "send" });
field(form, "textarea", { name: "body" }, { type: "textarea", value: "hello" });
field(form, "input", { name: "pw" }, { type: "password", value: "hunter2" });
field(form, "input", { name: "card_number" }, { type: "text", value: "4111111111111111" });
field(form, "input", { name: "agree" }, { type: "checkbox", checked: true, value: "yes" });
// Card numbers in fields named nothing like a card: one passes the Luhn check, one does not.
field(form, "input", { name: "ref" }, { type: "text", value: "4242 4242-4242 4242" });
field(form, "input", { name: "order_no" }, { type: "text", value: "1234567890123" });
// Names that only contain "pin" or "pass" inside a word are not secrets.
field(form, "input", { name: "shipping_address" }, { type: "text", value: "1 Main St" });
field(form, "input", { name: "passengers" }, { type: "text", value: "2" });
field(form, "input", { name: "pin" }, { type: "text", value: "1234" });
field(form, "input", { name: "color" }, { type: "radio", checked: false, value: "red" });
const send = field(form, "button", { type: "submit", id: "send", name: "op" }, { type: "submit", value: "send" });
bySel["#send span"] = send.add(new El("span", {}, { innerText: "Send" }));
bySel["#send"] = send;
bySel["#login"] = form;
bySel["#toggle"] = field(form, "button", { type: "button", id: "toggle" }, { type: "button", innerText: "Show password" });
for (let i = 0; i < 64; i++) field(form, "input", { name: "extra" + i }, { type: "text", value: "v" + i });
// Fields FormData leaves out: a nameless input (React reads it), an editor
// synced to a hidden input on submit, a nameless checkbox and select.
bySel["#note"] = field(form, "input", { id: "note", placeholder: "Note to seller" }, { type: "text", value: "leave at door", placeholder: "Note to seller" });
const nameless = field(form, "input", {}, { type: "text", value: "react state" });
bySel["#rich"] = form.add(new El("div", { contenteditable: "true", id: "rich", "aria-label": "Message" }, { isContentEditable: true, innerText: "rich text" }));
bySel["#gift"] = field(form, "input", { id: "gift" }, { type: "checkbox", checked: false });
const size = field(form, "select", { id: "size" }, { type: "select-one" });
size.selectedOptions = [{ value: "m", text: "Medium" }];
bySel["#menu"] = body.add(new El("button", { id: "menu" }, { innerText: "Open menu" }));
bySel["#publish"] = body.add(new El("a", { id: "publish" }, { innerText: "Publish now" }));
bySel["#big"] = body.add(new El("div", { id: "big" }, { innerText: "Publish ".repeat(20) }));
bySel["#sub"] = body.add(new El("input", { type: "submit", id: "sub" }, { type: "submit", value: "Go" }));
// A composer with no form: an editor and a Post button in one box.
const composer = body.add(new El("section"));
bySel["#editor"] = composer.add(new El("div", { contenteditable: "true", id: "editor" }, { isContentEditable: true, innerText: "draft" }));
bySel["#post"] = composer.add(new El("button", { id: "post" }, { innerText: "Post" }));
// Another: the button sits in a toolbar beside a checkbox, after the editor.
const composer2 = body.add(new El("section"));
bySel["#editor2"] = composer2.add(new El("textarea", { id: "editor2" }, { type: "textarea", value: "second draft" }));
const toolbar = composer2.add(new El("div"));
bySel["#notify"] = toolbar.add(new El("input", { id: "notify" }, { type: "checkbox", checked: true }));
bySel["#post2"] = toolbar.add(new El("button", { id: "post2" }, { innerText: "Post" }));
`

// page is a fresh probeDOM, then setup.
func page(t *testing.T, setup string) *goja.Runtime {
	t.Helper()
	vm := goja.New()
	if _, err := vm.RunString(probeDOM + setup); err != nil {
		t.Fatalf("dom: %v", err)
	}
	return vm
}

func run(t *testing.T, vm *goja.Runtime, js string, out any) {
	t.Helper()
	v, err := vm.RunString(js)
	if err != nil {
		t.Fatalf("script: %v", err)
	}
	raw, _ := json.Marshal(v.Export())
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("answer %s: %v", raw, err)
	}
}

func runProbe(t *testing.T, selector string) SubmitTarget {
	t.Helper()
	var st SubmitTarget
	run(t, page(t, ""), pageScript(probeSubmitJS, "", selector, ""), &st)
	return st
}

func holdDigest(t *testing.T, setup, form, sub string) string {
	t.Helper()
	var h held
	run(t, page(t, setup), pageScript(holdJS, form, sub, ""), &h)
	if !h.Found || len(h.Digest) != 64 {
		t.Fatalf("hold %q %q after %q: %+v", form, sub, setup, h)
	}
	return h.Digest
}

func TestPageSHA256MatchesGo(t *testing.T) {
	vm := page(t, "")
	for _, s := range []string{"", "abc", strings.Repeat("0123456789", 100), "héllo – 😀  ", strings.Repeat("x", 55), strings.Repeat("y", 56)} {
		v, err := vm.RunString("(() => {" + formLibJS + "return sha256(" + mustJSON(s) + "); })()")
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(s))
		if got := v.String(); got != hex.EncodeToString(sum[:]) {
			t.Errorf("sha256(%q) = %s, want %x", s, got, sum)
		}
	}
}

func mustJSON(s string) string { b, _ := json.Marshal(s); return string(b) }

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
		{"#post", true, "its text"},
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
	f := runProbe(t, "#send").Form
	if f.Action != "https://example.com/post" || f.Method != "post" || f.Selector != "#login" {
		t.Fatalf("form = %+v", f)
	}
	values := map[string]string{}
	for _, fl := range f.Fields {
		values[fl.Name] = fl.Value
	}
	want := map[string]string{"action": "•••", "body": "hello", "pw": "•••", "card_number": "•••", "agree": "yes",
		"ref": "•••", "order_no": "1234567890123", "shipping_address": "1 Main St", "passengers": "2", "pin": "•••"}
	for k, v := range want {
		if values[k] != v {
			t.Errorf("field %s = %q, want %q", k, values[k], v)
		}
	}
	if _, ok := values["color"]; ok {
		t.Error("an unchecked radio is listed")
	}
	if len(f.Fields) > 60 {
		t.Errorf("the card lists %d fields", len(f.Fields))
	}
	if got := runProbe(t, "#menu"); got.Form.Selector != "" || len(got.Form.Fields) != 0 {
		t.Errorf("a control outside a form has a form: %+v", got.Form)
	}
}

// The digest covers what the submit would send, unmasked and in full: a
// change to a hidden or password field, past the card's 200 characters or
// 60 fields, or to the submitter's value changes it; a label does not.
func TestHeldDigestCoversWhatTheSubmitSends(t *testing.T) {
	base := holdDigest(t, "", "#login", "#send")
	if again := holdDigest(t, "", "#login", "#send span"); again != base {
		t.Fatal("a click inside the button binds differently from the button")
	}
	for name, setup := range map[string]string{
		"hidden input":    `form.querySelectorAll("input")[0].value = "delete";`,
		"password":        `form.querySelectorAll("input")[1].value = "hunter3";`,
		"past char 200":   `form.querySelector("textarea").value = "x".repeat(300) + "y";`,
		"field 61+":       `form.querySelectorAll("input").find((f) => f.name === "extra63").value = "changed";`,
		"submitter value": `send.value = "send-all";`,
		"action":          `form.attrs.action = "/elsewhere";`,
		"formaction":      `send.attrs.formaction = "/elsewhere";`,
	} {
		if got := holdDigest(t, setup, "#login", "#send"); got == base {
			t.Errorf("%s: the digest did not change", name)
		}
	}
	if got := holdDigest(t, `form.querySelector("textarea").labels = [{ innerText: "Message" }];`, "#login", "#send"); got != base {
		t.Error("a label changed the digest")
	}
	long := holdDigest(t, `form.querySelector("textarea").value = "x".repeat(300);`, "#login", "#send")
	if holdDigest(t, `form.querySelector("textarea").value = "x".repeat(299) + "z";`, "#login", "#send") == long {
		t.Error("a change past the card's 200 characters kept the digest")
	}
}

func TestHeldDigestOfAControlOutsideAForm(t *testing.T) {
	base := holdDigest(t, "", "", "#post")
	if got := holdDigest(t, `bySel["#editor"].innerText = "edited";`, "", "#post"); got == base {
		t.Fatal("editing the composer kept the digest")
	}
}

// The trigger checks the digest and submits in one evaluation.
func TestTriggerChecksAndSubmitsInOneStep(t *testing.T) {
	vm := page(t, "")
	var h held
	run(t, vm, pageScript(holdJS, "#login", "#send", ""), &h)
	var out struct {
		OK      bool   `json:"ok"`
		Changed bool   `json:"changed"`
		Missing string `json:"missing"`
	}
	run(t, vm, pageScript(triggerJS, "#login", "#send", h.Digest), &out)
	if !out.OK || vm.Get("clicks").String() != "send" {
		t.Fatalf("trigger: %+v, clicks %v", out, vm.Get("clicks"))
	}
	if _, err := vm.RunString(`form.querySelectorAll("input")[1].value = "changed";`); err != nil {
		t.Fatal(err)
	}
	out.OK = false
	run(t, vm, pageScript(triggerJS, "#login", "#send", h.Digest), &out)
	if out.OK || !out.Changed || vm.Get("clicks").String() != "send" {
		t.Fatalf("trigger after a change: %+v, clicks %v", out, vm.Get("clicks"))
	}
	out.Changed = false
	run(t, vm, pageScript(triggerJS, "#login", "", ""), &out)
	if !out.OK || vm.Get("submits").String() != "login" {
		t.Fatalf("requestSubmit with no check: %+v, submits %v", out, vm.Get("submits"))
	}
	run(t, vm, pageScript(triggerJS, "#login", "#gone", h.Digest), &out)
	if out.Missing == "" {
		t.Fatalf("a missing control: %+v", out)
	}
}

// Fields FormData leaves out are bound too, and the card lists them by
// their label or position.
func TestHeldDigestCoversFieldsWithoutNames(t *testing.T) {
	base := holdDigest(t, "", "#login", "#send")
	for name, setup := range map[string]string{
		"nameless input":    `nameless.value = "other state";`,
		"input with an id":  `bySel["#note"].value = "ring twice";`,
		"rich-text editor":  `bySel["#rich"].innerText = "edited";`,
		"unchecked box":     `bySel["#gift"].checked = true;`,
		"nameless select":   `size.selectedOptions = [{ value: "l", text: "Large" }];`,
	} {
		if got := holdDigest(t, setup, "#login", "#send"); got == base {
			t.Errorf("%s: the digest did not change", name)
		}
	}
}

func TestTheCardListsFieldsWithoutNames(t *testing.T) {
	var h held
	run(t, page(t, `for (const f of form.querySelectorAll("input")) if (/^extra/.test(f.name)) form.kids.splice(form.kids.indexOf(f), 1);`),
		pageScript(holdJS, "#login", "#send", ""), &h)
	byName := map[string]FormField{}
	for _, f := range h.Form.Fields {
		byName[f.Name] = f
	}
	if f := byName["note"]; f.Value != "leave at door" {
		t.Errorf("the input with only an id: %+v", f)
	}
	if f := byName["rich"]; f.Type != "contenteditable" || f.Label != "Message" || f.Value != "rich text" {
		t.Errorf("the editor: %+v (fields %+v)", f, h.Form.Fields)
	}
	found := false
	for _, f := range h.Form.Fields {
		found = found || (strings.HasPrefix(f.Name, "field ") && f.Value == "react state")
	}
	if !found {
		t.Errorf("the nameless input is not listed by position: %+v", h.Form.Fields)
	}
}

// Outside a form: the composer the control posts (a text field before it),
// not the nearest box with any field; a checkbox's state counts.
func TestHeldDigestOfAComposerAroundAToolbar(t *testing.T) {
	base := holdDigest(t, "", "", "#post2")
	if got := holdDigest(t, `bySel["#editor2"].value = "changed draft";`, "", "#post2"); got == base {
		t.Error("the editor before the toolbar is not bound")
	}
	if got := holdDigest(t, `bySel["#notify"].checked = false;`, "", "#post2"); got == base {
		t.Error("the checkbox's state is not bound")
	}
}
