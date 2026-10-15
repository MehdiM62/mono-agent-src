package browserctl

import (
	"encoding/json"
	"strings"
)

// The page scripts of the submit gate. formLibJS is shared:
//
//   - summarize: a form's action, method and fields for the approval card.
//     Sensitive values are masked the way submit_guard masks them (password
//     and hidden fields, card-like autocomplete, a name or id saying card,
//     cvv, iban, ssn, secret, token, passw, otp or the word pin, and any
//     value holding a card number: 13 to 19 digits passing the Luhn check).
//     It shows at most 60 fields of at most 200 characters.
//   - digestOf: what an approval is bound to, a SHA-256 computed in the page
//     over everything the submit would send, unmasked and untruncated: the
//     FormData entries in order (files as name, size and type), the action,
//     method and enctype with the submitter's overrides, and the submitter's
//     formaction, name and value. A control outside any form is bound to the
//     values of the fields in its nearest container. Only the hash leaves
//     the page.
//
// Each script starts with a /*mono:…*/ marker naming it.
const formLibJS = `
  const unique = (id) => id && document.querySelectorAll("#" + CSS.escape(id)).length === 1;
  const cssPath = (n) => {
    const parts = [];
    while (n && n.nodeType === 1 && n !== document.documentElement) {
      if (unique(n.id)) { parts.unshift("#" + CSS.escape(n.id)); break; }
      let i = 1, sib = n;
      while ((sib = sib.previousElementSibling)) if (sib.tagName === n.tagName) i++;
      parts.unshift(n.tagName.toLowerCase() + ":nth-of-type(" + i + ")");
      n = n.parentElement;
    }
    return parts.join(" > ");
  };
  const luhn = (d) => {
    let sum = 0;
    for (let i = d.length - 1, alt = false; i >= 0; i--, alt = !alt) {
      let n = d.charCodeAt(i) - 48;
      if (alt) { n *= 2; if (n > 9) n -= 9; }
      sum += n;
    }
    return sum % 10 === 0;
  };
  const sensitive = (el, type, value) => {
    if (type === "password" || type === "hidden") return true;
    const ac = String(el.getAttribute("autocomplete") || "").toLowerCase();
    if (/(^|\s)(cc-|current-password|new-password|one-time-code)/.test(ac)) return true;
    const named = ((el.name || "") + " " + (el.id || "")).toLowerCase();
    if (/card|cc[-_]?(num|number|exp|csc|cvv)|cvv|cvc|csc|iban|ssn|secret|token|passw|otp|(^|[^a-z])pin([^a-z]|$)/.test(named)) return true;
    const runs = String(value).match(/\d(?:[ -]?\d){12,18}/g) || [];
    return runs.some((run) => luhn(run.replace(/[ -]/g, "")));
  };
  const elementsOf = (form) => Array.from(Object.getOwnPropertyDescriptor(HTMLFormElement.prototype, "elements").get.call(form));
  const summarize = (form) => {
    if (!form) return { action: "", method: "", fields: [] };
    const fields = [];
    // Through the prototype: a field named "elements" or "action" shadows the form's own.
    for (const f of elementsOf(form).slice(0, 60)) {
      const ftype = (f.type || f.tagName || "").toLowerCase();
      if (!(f.name || f.id) || ["submit", "button", "reset", "image", "fieldset", "object", "output"].includes(ftype)) continue;
      if (ftype === "radio" && !f.checked) continue;
      let label = f.labels && f.labels[0] ? f.labels[0].innerText : (f.getAttribute("aria-label") || f.placeholder || "");
      label = String(label).trim().slice(0, 80);
      let value;
      if (ftype === "checkbox" || ftype === "radio") value = f.checked ? (f.value || "on") : "";
      else if (ftype === "file") value = Array.from(f.files || []).map((x) => x.name).join(", ");
      else if (f.tagName === "SELECT") value = Array.from(f.selectedOptions).map((o) => o.text).join(", ");
      else value = String(f.value || "");
      if (value && sensitive(f, ftype, value)) value = "•••";
      if (value.length > 200) value = value.slice(0, 200) + "…";
      fields.push({ name: f.name || f.id, type: ftype, label, value });
    }
    return { action: new URL(form.getAttribute("action") || "", location.href).href,
             method: (form.getAttribute("method") || "get").toLowerCase(), fields, selector: cssPath(form) };
  };
  const sha256 = (s) => {
    const b = [];
    for (const ch of s) {
      const c = ch.codePointAt(0);
      if (c < 0x80) b.push(c);
      else if (c < 0x800) b.push(0xc0 | (c >> 6), 0x80 | (c & 63));
      else if (c < 0x10000) b.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
      else b.push(0xf0 | (c >> 18), 0x80 | ((c >> 12) & 63), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
    }
    const K = [0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
      0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
      0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
      0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
      0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
      0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
      0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
      0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2];
    const H = [0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19];
    const n = b.length;
    b.push(0x80);
    while (b.length % 64 !== 56) b.push(0);
    for (let i = 7; i >= 0; i--) b.push(Math.floor((n * 8) / Math.pow(2, i * 8)) & 255);
    const r = (x, k) => (x >>> k) | (x << (32 - k));
    const w = new Array(64);
    for (let o = 0; o < b.length; o += 64) {
      for (let i = 0; i < 16; i++) w[i] = (b[o + 4 * i] << 24) | (b[o + 4 * i + 1] << 16) | (b[o + 4 * i + 2] << 8) | b[o + 4 * i + 3];
      for (let i = 16; i < 64; i++) {
        const s0 = r(w[i - 15], 7) ^ r(w[i - 15], 18) ^ (w[i - 15] >>> 3);
        const s1 = r(w[i - 2], 17) ^ r(w[i - 2], 19) ^ (w[i - 2] >>> 10);
        w[i] = (w[i - 16] + s0 + w[i - 7] + s1) | 0;
      }
      let a = H[0], bb = H[1], c = H[2], d = H[3], e = H[4], f = H[5], g = H[6], h = H[7];
      for (let i = 0; i < 64; i++) {
        const t1 = (h + (r(e, 6) ^ r(e, 11) ^ r(e, 25)) + ((e & f) ^ (~e & g)) + K[i] + w[i]) | 0;
        const t2 = ((r(a, 2) ^ r(a, 13) ^ r(a, 22)) + ((a & bb) ^ (a & c) ^ (bb & c))) | 0;
        h = g; g = f; f = e; e = (d + t1) | 0; d = c; c = bb; bb = a; a = (t1 + t2) | 0;
      }
      H[0] = (H[0] + a) | 0; H[1] = (H[1] + bb) | 0; H[2] = (H[2] + c) | 0; H[3] = (H[3] + d) | 0;
      H[4] = (H[4] + e) | 0; H[5] = (H[5] + f) | 0; H[6] = (H[6] + g) | 0; H[7] = (H[7] + h) | 0;
    }
    return H.map((x) => ("00000000" + (x >>> 0).toString(16)).slice(-8)).join("");
  };
  const ctlOf = (el) => (el ? el.closest("button,input,a,[role=button]") || el : null);
  const FIELDS = "input,textarea,select,[contenteditable]";
  const digestOf = (form, ctl) => {
    const attr = (el, a) => (el && el.getAttribute(a)) || "";
    if (form) {
      const sub = ctl && ctl.form === form && /^(submit|image)$/i.test(ctl.type || "") ? ctl : null;
      let fd;
      try { fd = sub ? new FormData(form, sub) : new FormData(form); } catch (e) { fd = new FormData(form); }
      const entries = [];
      for (const [k, v] of fd.entries()) entries.push([k, typeof v === "string" ? v : { file: v.name, size: v.size, type: v.type }]);
      return sha256(JSON.stringify({
        action: new URL(attr(sub, "formaction") || attr(form, "action") || "", location.href).href,
        method: (attr(sub, "formmethod") || attr(form, "method") || "get").toLowerCase(),
        enctype: (attr(sub, "formenctype") || attr(form, "enctype") || "").toLowerCase(),
        submitter: ctl ? [attr(ctl, "formaction"), ctl.name || "", typeof ctl.value === "string" ? ctl.value : ""] : null,
        entries,
      }));
    }
    let box = ctl ? ctl.parentElement : null;
    while (box && box !== document.body && !box.querySelector(FIELDS)) box = box.parentElement;
    const els = box ? Array.from(box.querySelectorAll(FIELDS)) : [];
    return sha256(JSON.stringify({
      control: ctl ? [ctl.tagName, ctl.name || "", typeof ctl.value === "string" ? ctl.value : ""] : null,
      values: els.map((e) => [e.name || e.id || "", e.type || e.tagName, e.isContentEditable ? e.innerText : String(e.value == null ? "" : e.value)]),
    }));
  };
`

// probeSubmitJS answers ProbeSubmit for the control at __MONO_SUB__.
const probeSubmitJS = `/*mono:probe*/(() => {` + formLibJS + `
  const el = document.querySelector(__MONO_SUB__);
  if (!el) return { found: false, submit: false, form: summarize(null) };
  const ctl = ctlOf(el);
  const form = ctl.form || ctl.closest("form");
  const tag = ctl.tagName;
  const type = (ctl.getAttribute("type") || "").toLowerCase();
  let why = "";
  if ((tag === "INPUT" || tag === "BUTTON") && (type === "submit" || type === "image")) why = "type=submit";
  else if (tag === "BUTTON" && form) why = "a button inside a form";
  else {
    const label = [ctl.innerText || "", typeof ctl.value === "string" ? ctl.value : "", ctl.getAttribute("aria-label") || ""].join(" ").trim();
    if (label.length <= 80 && /\b(submit|send|post|publish|pay|buy|confirm|order|sign\s*up|apply)\b/i.test(label)) why = "its text";
  }
  return { found: true, submit: !!why, why, form: summarize(form) };
})()`

// holdJS reads what a held submit is bound to: the form at __MONO_FORM__ (none
// when "") with the submit control at __MONO_SUB__. found is false when a named
// form or control is gone.
const holdJS = `/*mono:hold*/(() => {` + formLibJS + `
  const form = __MONO_FORM__ ? document.querySelector(__MONO_FORM__) : null;
  const el = __MONO_SUB__ ? document.querySelector(__MONO_SUB__) : null;
  if ((__MONO_FORM__ && !(form instanceof HTMLFormElement)) || (__MONO_SUB__ && !el)) return { found: false, form: summarize(null), digest: "" };
  const ctl = ctlOf(el);
  return { found: true, form: summarize(form), digest: digestOf(form, ctl) };
})()`

// triggerJS repeats a held submit only if what it would send still has the
// digest __MONO_EXPECT__ ("" checks nothing): the check and the click (or
// requestSubmit) run in one evaluation, so nothing can change in between.
const triggerJS = `/*mono:trigger*/(() => {` + formLibJS + `
  const EXPECT = __MONO_EXPECT__;
  const form = __MONO_FORM__ ? document.querySelector(__MONO_FORM__) : null;
  const el = __MONO_SUB__ ? document.querySelector(__MONO_SUB__) : null;
  if (__MONO_FORM__ && !(form instanceof HTMLFormElement)) return { missing: "the form" };
  if (__MONO_SUB__ && !el) return { missing: "the submit control" };
  const ctl = ctlOf(el);
  if (EXPECT && digestOf(form, ctl) !== EXPECT) return { changed: true };
  if (el) el.click();
  else form.requestSubmit();
  return { ok: true };
})()`

// pageScript fills a script's __MONO_FORM__, __MONO_SUB__ and
// __MONO_EXPECT__ with JSON strings, in one pass (a value is never scanned
// for placeholders).
func pageScript(js, form, sub, expect string) string {
	enc := func(v string) string { b, _ := json.Marshal(v); return string(b) }
	return strings.NewReplacer("__MONO_FORM__", enc(form), "__MONO_SUB__", enc(sub), "__MONO_EXPECT__", enc(expect)).Replace(js)
}
