package browserctl

// probeSubmitJS answers ProbeSubmit for the selector %s (a JSON string),
// evaluated in the page's main world. Sensitive values are masked the way
// submit_guard masks them: password, hidden and card-like fields, and any
// whose name, id or label says pass/pin/cvv/otp/ssn/token/secret/card.
const probeSubmitJS = `(() => {
  const el = document.querySelector(%s);
  if (!el) return { found: false, submit: false, form: { action: "", method: "", fields: [] } };
  const ctl = el.closest("button,input,a,[role=button]") || el;
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
  const SENS = /pass|pwd|pin|cvv|cvc|csc|otp|ssn|token|secret|card|iban/i;
  const fields = [];
  if (form) {
    // Through the prototype: a field named "elements" or "action" shadows the form's own.
    const all = Object.getOwnPropertyDescriptor(HTMLFormElement.prototype, "elements").get.call(form);
    for (const f of Array.from(all).slice(0, 60)) {
      const ftype = (f.type || f.tagName || "").toLowerCase();
      if (!(f.name || f.id) || ["submit", "button", "reset", "image", "fieldset", "object", "output"].includes(ftype)) continue;
      if (ftype === "radio" && !f.checked) continue;
      const ac = (f.getAttribute("autocomplete") || "").toLowerCase();
      let label = f.labels && f.labels[0] ? f.labels[0].innerText : (f.getAttribute("aria-label") || f.placeholder || "");
      label = String(label).trim().slice(0, 80);
      let value;
      if (ftype === "checkbox" || ftype === "radio") value = f.checked ? (f.value || "on") : "";
      else if (ftype === "file") value = Array.from(f.files || []).map((x) => x.name).join(", ");
      else if (f.tagName === "SELECT") value = Array.from(f.selectedOptions).map((o) => o.text).join(", ");
      else value = String(f.value || "");
      const sensitive = ftype === "password" || ftype === "hidden" || /^cc-|password|one-time-code/.test(ac) ||
        SENS.test([f.name, f.id, label].join(" "));
      if (sensitive && value) value = "•••";
      if (value.length > 200) value = value.slice(0, 200) + "…";
      fields.push({ name: f.name || f.id, type: ftype, label, value });
    }
  }
  return {
    found: true, submit: !!why, why,
    form: form ? { action: new URL(form.getAttribute("action") || "", location.href).href,
                   method: (form.getAttribute("method") || "get").toLowerCase(), fields, selector: cssPath(form) }
               : { action: "", method: "", fields: [] },
  };
})()`
