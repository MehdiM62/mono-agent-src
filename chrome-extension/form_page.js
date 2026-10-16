/**
 * MonoAgent Bridge — the form filler's page half
 *
 * Injected into the person's tab on demand (form_bridge.js), never listed in
 * the manifest. Two things only:
 *
 *   scan()          describes the fillable fields: an id the page half stamps
 *                   on the element, its label, name, type, autocomplete,
 *                   placeholder and, for a select, its options.
 *   apply(values)   writes {id, value} answers back into those fields, the
 *                   way a person typing would, so frameworks that watch input
 *                   events (React, Vue, Angular) see them.
 *
 * It never clicks a button, never submits, and never touches a password,
 * hidden or file field. A text field the person has already typed in is left
 * alone; a select is set because its first value is only a default.
 *
 * The page is untrusted: its text is read as data and sent to the bridge,
 * which fences it before any model sees it.
 */

(function (root) {
  "use strict";

  const ATTR = "data-mono-fill";
  const SKIP_TYPES = new Set(["password", "hidden", "file", "submit", "button", "reset", "image", "checkbox", "radio", "range", "color"]);
  const MAX_FIELDS = 80;

  const clean = (s) => String(s == null ? "" : s).replace(/\s+/g, " ").trim();

  function visible(el) {
    if (!el.getClientRects().length) return false;
    const st = root.getComputedStyle(el);
    return st.visibility !== "hidden" && st.display !== "none";
  }

  function labelOf(el) {
    const doc = el.ownerDocument;
    if (el.id) {
      const l = doc.querySelector('label[for="' + root.CSS.escape(el.id) + '"]');
      if (l) return clean(l.textContent);
    }
    const wrap = el.closest("label");
    if (wrap) return clean(wrap.textContent);
    const by = el.getAttribute("aria-labelledby");
    if (by) {
      const t = by.split(/\s+/).map((id) => (doc.getElementById(id) || {}).textContent || "").join(" ");
      if (clean(t)) return clean(t);
    }
    return clean(el.getAttribute("aria-label"));
  }

  function fieldType(el) {
    if (el.tagName === "SELECT") return "select";
    if (el.tagName === "TEXTAREA") return "textarea";
    return (el.getAttribute("type") || "text").toLowerCase();
  }

  function scan() {
    const out = [];
    const els = document.querySelectorAll("input, textarea, select");
    let n = 0;
    for (const el of els) {
      if (out.length >= MAX_FIELDS) break;
      const type = fieldType(el);
      if (SKIP_TYPES.has(type) || el.disabled || el.readOnly || !visible(el)) continue;
      const id = "m" + ++n;
      el.setAttribute(ATTR, id);
      const f = {
        id,
        label: labelOf(el),
        name: el.getAttribute("name") || el.id || "",
        type,
        autocomplete: el.getAttribute("autocomplete") || "",
        placeholder: el.getAttribute("placeholder") || "",
        required: !!el.required,
        filled: type !== "select" && !!el.value,
      };
      if (el.maxLength > 0 && el.maxLength < 1000) f.maxlength = el.maxLength;
      if (type === "select") {
        f.options = Array.from(el.options).map((o) => clean(o.text)).filter(Boolean);
      }
      out.push(f);
    }
    return { url: location.href, title: document.title, fields: out };
  }

  // The framework's own setter: assigning el.value directly is swallowed by
  // React's value tracker, so the change would never register.
  function setNative(el, value) {
    const proto = el.tagName === "TEXTAREA" ? root.HTMLTextAreaElement.prototype : el.tagName === "SELECT" ? root.HTMLSelectElement.prototype : root.HTMLInputElement.prototype;
    const setter = Object.getOwnPropertyDescriptor(proto, "value").set;
    setter.call(el, value);
  }

  function apply(values) {
    let filled = 0;
    for (const v of Array.isArray(values) ? values : []) {
      if (!v || typeof v.id !== "string" || typeof v.value !== "string") continue;
      const el = document.querySelector("[" + ATTR + '="' + root.CSS.escape(v.id) + '"]');
      if (!el || el.disabled || el.readOnly) continue;
      if (el.tagName === "SELECT") {
        const opt = Array.from(el.options).find((o) => clean(o.text) === v.value);
        if (!opt) continue;
        setNative(el, opt.value);
      } else {
        if (el.value) continue;
        el.focus();
        setNative(el, v.value);
      }
      el.dispatchEvent(new Event("input", { bubbles: true }));
      el.dispatchEvent(new Event("change", { bubbles: true }));
      filled++;
    }
    for (const el of document.querySelectorAll("[" + ATTR + "]")) el.removeAttribute(ATTR);
    return { filled };
  }

  // The words near a file box that has no label of its own: a custom upload
  // button is usually a div with a caption next to a hidden <input type=file>.
  function captionOf(el) {
    const own = labelOf(el);
    if (own) return own;
    const box = el.closest("label, li, fieldset, div, section");
    return box ? clean(box.textContent).slice(0, 120) : "";
  }

  // File inputs are often hidden behind a styled button, so unlike text
  // fields they need not be visible.
  function scanFiles() {
    const out = [];
    let n = 0;
    for (const el of document.querySelectorAll('input[type="file"]')) {
      if (out.length >= 12) break;
      if (el.disabled) continue;
      const id = "f" + ++n;
      el.setAttribute(ATTR, id);
      out.push({ id, label: captionOf(el), name: el.getAttribute("name") || el.id || "", accept: el.getAttribute("accept") || "" });
    }
    return { url: location.href, files: out };
  }

  // Puts files into the file boxes the bridge matched, as if chosen in the
  // browser's own dialog: a DataTransfer carries them, and the page is told.
  function applyFiles(files) {
    let attached = 0;
    for (const f of Array.isArray(files) ? files : []) {
      if (!f || typeof f.id !== "string" || typeof f.data !== "string") continue;
      const el = document.querySelector("[" + ATTR + '="' + root.CSS.escape(f.id) + '"]');
      if (!el || el.disabled || (el.files && el.files.length)) continue;
      const bin = root.atob(f.data);
      const bytes = new Uint8Array(bin.length);
      for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
      const dt = new root.DataTransfer();
      dt.items.add(new root.File([bytes], f.name || "file", { type: f.mime || "" }));
      el.files = dt.files;
      el.dispatchEvent(new Event("input", { bubbles: true }));
      el.dispatchEvent(new Event("change", { bubbles: true }));
      attached++;
    }
    for (const el of document.querySelectorAll("[" + ATTR + "]")) el.removeAttribute(ATTR);
    return { attached };
  }

  root.MonoFormPage = { scan, apply, scanFiles, applyFiles };
})(globalThis);
