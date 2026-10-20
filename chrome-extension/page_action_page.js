/**
 * MonoAgent Bridge — a chat page action, the page's half
 *
 * Injected into the person's tab on demand (page_action_bridge.js), never
 * listed in the manifest. The chat's card names an element by its words
 * ("Double-click “CV - Morteza Nokhodian 2025 Apr”"); this finds it and does
 * the one thing the person approved:
 *
 *   run(action, target, text)  click | double_click | type
 *     -> {ok:true} | {ok:false, code, error}
 *        codes: not_found, ambiguous (N matches), submit_blocked, not_allowed
 *
 * Finding it: visible, enabled elements only; the words are an element's
 * aria-label, accessible name (aria-labelledby, its label), placeholder,
 * title, button value or text, compared case-insensitively. An exact match
 * wins; only when nothing matches exactly does a longer text that contains
 * the words count. Two equally good matches are never guessed between.
 *
 * A click that could submit a form is not made (decision: the extension never
 * submits; the person presses the button): a submit control, a button inside
 * a form, or a button whose words say submit/send/post/publish/pay/buy/
 * confirm/order/sign up/apply. A file called "Purchase order.pdf" is not a
 * button, so it is not held up by its name.
 *
 * resolve() and the guards are pure functions of the DOM, so the browser test
 * runs them on real pages (page_action_page.browser.test.mjs). guard() is
 * self-contained: the debugger fallback runs its source in the page
 * (page_action_cdp.js).
 */

(function (root) {
  "use strict";

  const MAX_ELEMENTS = 20000;
  const MAX_MATCHES = 200;
  // What a click is aimed at when the words matched something inside it.
  const ACTIONABLE =
    'a[href],button,input,select,textarea,summary,label,[role="button"],[role="link"],[role="option"],[role="menuitem"],[role="tab"],[role="checkbox"],[role="radio"],[role="switch"],[role="row"],[role="gridcell"],[role="treeitem"],[role="listitem"],[role="textbox"],[role="searchbox"],[role="combobox"],[tabindex],[onclick],[contenteditable=""],[contenteditable="true"]';
  const SKIP_TAGS = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "HEAD", "META", "LINK", "TEMPLATE", "TITLE", "HTML"]);
  const NOT_TEXT_INPUTS = new Set(["checkbox", "radio", "submit", "button", "reset", "image", "file", "color", "range", "hidden"]);

  const norm = (s) => String(s == null ? "" : s).replace(/\s+/g, " ").trim().toLowerCase();

  function visible(el) {
    if (!el.getClientRects().length) return false;
    const st = root.getComputedStyle(el);
    return st.visibility !== "hidden" && st.display !== "none";
  }
  const enabled = (el) => !el.matches(":disabled") && el.getAttribute("aria-disabled") !== "true";

  function editable(el) {
    if (el.tagName === "TEXTAREA") return true;
    if (el.tagName === "INPUT") return !NOT_TEXT_INPUTS.has((el.getAttribute("type") || "text").toLowerCase());
    return el.isContentEditable || ["textbox", "searchbox", "combobox"].includes(el.getAttribute("role"));
  }

  /** wordsOf: what an element is called, by name; its text apart (text is held to a size). */
  function wordsOf(el) {
    const names = [];
    const add = (v) => {
      const n = norm(v);
      if (n) names.push(n);
    };
    add(el.getAttribute("aria-label"));
    const by = el.getAttribute("aria-labelledby");
    if (by) add(by.split(/\s+/).map((id) => (el.ownerDocument.getElementById(id) || {}).textContent || "").join(" "));
    if (el.labels) for (const l of el.labels) add(l.textContent);
    add(el.getAttribute("placeholder"));
    add(el.getAttribute("title"));
    if (el.tagName === "INPUT" && ["button", "submit", "reset"].includes((el.getAttribute("type") || "").toLowerCase())) add(el.value);
    if (el.tagName === "IMG") add(el.getAttribute("alt"));
    return { names, text: el.children.length > 200 ? "" : norm(el.textContent) };
  }

  /** resolve finds the elements `target` names for `action`, the best tier of matches: {elements, count}. */
  function resolve(doc, target, action) {
    const want = norm(target);
    const none = { elements: [], count: 0 };
    if (!want) return none;
    const longest = Math.max(want.length * 3, want.length + 60);
    const exact = [];
    const partial = [];
    let seen = 0;
    for (const el of doc.querySelectorAll("*")) {
      if (++seen > MAX_ELEMENTS) break;
      if (SKIP_TAGS.has(el.tagName)) continue;
      if (action === "type" && !editable(el)) continue;
      const w = wordsOf(el);
      const isExact = w.names.includes(want) || w.text === want;
      const isPartial = !isExact && (w.names.some((n) => n.includes(want)) || (w.text.length <= longest && w.text.includes(want)));
      if (!isExact && !isPartial) continue;
      if (!visible(el) || !enabled(el)) continue;
      (isExact ? exact : partial).push(el);
    }
    const tier = exact.length ? exact : partial;
    // The innermost of nested matches, then what a click on it lands on, once.
    const inner = tier.length > MAX_MATCHES ? tier : tier.filter((el) => !tier.some((o) => o !== el && el.contains(o)));
    const landing = (el) => {
      const t = el.closest(ACTIONABLE) || el;
      // A label and its control are one target.
      return t.tagName === "LABEL" && t.control && visible(t.control) ? t.control : t;
    };
    const targets = new Set(inner.map((el) => (action === "type" ? el : landing(el))));
    return { elements: [...targets], count: targets.size };
  }

  /**
   * guard(action) is run as the element (this): "" when the action may be
   * made, else submit_blocked or not_allowed. Self-contained.
   */
  function guard(action) {
    const el = this;
    const tag = el.tagName;
    const type = (el.getAttribute("type") || "").toLowerCase();
    if (action === "type") return tag === "INPUT" && ["password", "file", "hidden"].includes(type) ? "not_allowed" : "";
    const buttonLike = tag === "BUTTON" || el.getAttribute("role") === "button" || (tag === "INPUT" && ["button", "submit", "image", "reset"].includes(type));
    if (type === "submit" || (tag === "INPUT" && type === "image")) return "submit_blocked";
    if (!buttonLike) return "";
    if (el.form || el.closest("form")) return "submit_blocked";
    const said = [el.getAttribute("aria-label"), el.textContent, el.value, el.getAttribute("title")].filter(Boolean).join(" ");
    return /\b(submit|send|post|publish|pay|buy|confirm|order|sign ?up|apply)\b/i.test(said) ? "submit_blocked" : "";
  }

  function pointer(el, type, detail, init) {
    const r = el.getBoundingClientRect();
    const opts = Object.assign(
      { bubbles: true, cancelable: true, composed: true, view: root, detail, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2, button: 0 },
      init
    );
    const Ctor = type.startsWith("pointer") && root.PointerEvent ? root.PointerEvent : root.MouseEvent;
    if (Ctor === root.PointerEvent) Object.assign(opts, { pointerId: 1, pointerType: "mouse", isPrimary: true });
    el.dispatchEvent(new Ctor(type, opts));
  }

  function press(el, detail) {
    pointer(el, "pointerdown", detail, { buttons: 1 });
    pointer(el, "mousedown", detail, { buttons: 1 });
    if (el.focus) el.focus({ preventScroll: true });
    pointer(el, "pointerup", detail, { buttons: 0 });
    pointer(el, "mouseup", detail, { buttons: 0 });
    pointer(el, "click", detail, { buttons: 0 });
  }

  function typeInto(el, text) {
    el.focus();
    if (el.tagName === "INPUT" || el.tagName === "TEXTAREA") {
      // The native setter, so a framework that wraps .value still sees the change.
      const set = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el), "value").set;
      set.call(el, text);
      el.dispatchEvent(new InputEvent("input", { bubbles: true, inputType: "insertText", data: text }));
      el.dispatchEvent(new Event("change", { bubbles: true }));
      return;
    }
    const sel = root.getSelection();
    sel.selectAllChildren(el);
    if (!el.ownerDocument.execCommand("insertText", false, text)) el.textContent = text;
    el.dispatchEvent(new InputEvent("input", { bubbles: true, inputType: "insertText", data: text }));
  }

  /** run does one approved action on the page; the result is plain data. */
  function run(action, target, text) {
    const found = resolve(root.document, target, action);
    if (found.count === 0) return { ok: false, code: "not_found", error: "not found" };
    if (found.count > 1) return { ok: false, code: "ambiguous", error: `ambiguous (${found.count} matches)` };
    const el = found.elements[0];
    const blocked = guard.call(el, action);
    if (blocked === "submit_blocked") return { ok: false, code: blocked, error: "that would submit a form, so it was not clicked: press that button yourself" };
    if (blocked) return { ok: false, code: blocked, error: "that field is not one to type into" };
    el.scrollIntoView({ block: "center", inline: "center" });
    if (action === "type") typeInto(el, String(text == null ? "" : text));
    else if (action === "double_click") {
      press(el, 1);
      press(el, 2);
      pointer(el, "dblclick", 2, { buttons: 0 });
    } else if (action === "click") press(el, 1);
    else return { ok: false, code: "invalid", error: "unknown action" };
    return { ok: true };
  }

  root.MonoPageAction = { run, resolve, guard, ACTIONABLE };
})(globalThis);
