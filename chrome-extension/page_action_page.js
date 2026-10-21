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
 * wins; only when nothing matches exactly does a name of at most twice the
 * length that contains the words as whole words count (click only, never
 * typing), and run() says what it matched. Two equally good matches are never guessed between.
 *
 * A click that could submit or change something is not made (decision: the
 * extension never submits; the person presses the button): see guard().
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

  const KEYS = new Set(["Tab", "Escape", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "PageUp", "PageDown", "Home", "End"]);
  const CHECKABLE = 'input[type="checkbox"],input[type="radio"],[role="checkbox"],[role="radio"],[role="switch"]';

  /** shadowOf: an element's shadow root, closed ones too (chrome.dom reaches them from an extension). */
  const shadowOf = (el) => {
    try {
      const dom = root.chrome && root.chrome.dom;
      return (dom && dom.openOrClosedShadowRoot ? dom.openOrClosedShadowRoot(el) : null) || el.shadowRoot || null;
    } catch {
      return el.shadowRoot || null;
    }
  };

  /** deepAll: every element under doc, through shadow roots, in document order. */
  function deepAll(doc) {
    const out = [];
    const walk = (parent) => {
      for (const el of parent.querySelectorAll("*")) {
        out.push(el);
        if (out.length > MAX_ELEMENTS) return;
        const sr = shadowOf(el);
        if (sr) walk(sr);
        if (out.length > MAX_ELEMENTS) return;
      }
    };
    walk(doc);
    return out;
  }

  /** closestDeep: el.closest that also leaves a shadow root through its host. */
  function closestDeep(el, sel) {
    for (let n = el; n; n = n.parentElement || (n.getRootNode && n.getRootNode().host) || null) if (n.matches && n.matches(sel)) return n;
    return null;
  }

  const squash = (s) => String(s == null ? "" : s).replace(/\s+/g, " ").trim();
  const low = (s) => s.toLowerCase();

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

  /** wordsOf: what an element is called, by name (as written); its text apart. */
  function wordsOf(el) {
    const names = [];
    const add = (v) => {
      const n = squash(v);
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
    return { names, text: el.children.length > 200 ? "" : squash(el.textContent) };
  }

  /** bounded: `needle` is in `hay` as whole words (not inside a longer word). Both lower case. */
  function bounded(hay, needle) {
    const letter = /[\p{L}\p{N}]/u;
    for (let i = hay.indexOf(needle); i !== -1; i = hay.indexOf(needle, i + 1)) {
      const before = hay[i - 1];
      const after = hay[i + needle.length];
      if ((!before || !letter.test(before)) && (!after || !letter.test(after))) return true;
    }
    return false;
  }

  /**
   * resolve finds the elements `target` names for `action`: the exact tier if
   * it has any, else (click and double_click only) whole-word containment in
   * a name or text no longer than twice the words. {elements, count, exact,
   * matched}: matched is what the one element is called, as written.
   */
  function resolve(doc, target, action) {
    const want = low(squash(target));
    const none = { elements: [], count: 0, exact: false, matched: "" };
    if (!want) return none;
    const exact = [];
    const partial = [];
    let seen = 0;
    for (const el of deepAll(doc)) {
      if (++seen > MAX_ELEMENTS) break;
      if (SKIP_TAGS.has(el.tagName)) continue;
      if (action === "type" && !editable(el)) continue;
      const w = wordsOf(el);
      const all = w.text ? [...w.names, w.text] : w.names;
      const whole = all.find((n) => low(n) === want);
      const part = whole === undefined && action !== "type" ? all.find((n) => n.length <= want.length * 2 && bounded(low(n), want)) : undefined;
      if (whole === undefined && part === undefined) continue;
      if (!visible(el) || !enabled(el)) continue;
      (whole !== undefined ? exact : partial).push([el, whole !== undefined ? whole : part]);
    }
    const tier = exact.length ? exact : partial;
    // The innermost of nested matches, then what a click on it lands on, once.
    const inner = tier.length > MAX_MATCHES ? tier : tier.filter(([el]) => !tier.some(([o]) => o !== el && el.contains(o)));
    const landing = (el) => {
      const t = closestDeep(el, ACTIONABLE) || el;
      // A label and its control are one target, hidden or not: the control is what the click reaches.
      if (t.tagName === "LABEL" && t.control) return t.control;
      // A text that a box names in aria-labelledby is that box.
      const named = t.id && t.getRootNode().querySelectorAll ? Array.from(t.getRootNode().querySelectorAll(CHECKABLE)).filter((b) => (b.getAttribute("aria-labelledby") || "").split(/\s+/).includes(t.id)) : [];
      return named.length === 1 ? named[0] : t;
    };
    const targets = new Map();
    for (const [el, name] of inner) {
      const t = action === "type" ? el : landing(el);
      if (!targets.has(t)) targets.set(t, name);
    }
    return { elements: [...targets.keys()], count: targets.size, exact: exact.length > 0, matched: [...targets.values()][0] || "" };
  }

  /**
   * guard(action) is run as the element (this): "" when the action may be
   * made, else submit_blocked or not_allowed. Self-contained (the debugger
   * path runs its source in the page).
   *
   * A click is held back when it could submit or change something: a submit
   * control; anything clickable inside a form (or a label whose control is);
   * or a button, link, menu item, [onclick] or input whose words (label,
   * labelledby text, text, value, title, image alt) are a submit-like verb in
   * English, German, French or Spanish. A plain row of text, such as a file
   * called "Purchase order.pdf", is not tested by its words.
   */
  function guard(action, extra) {
    const CLICKABLE = "a,button,input,select,textarea,summary,label,[role],[onclick],[tabindex]";
    const VERBS =
      "submit|send|post|publish|pay|buy|confirm|order|sign ?up|apply|delete|remove|archive|unsubscribe|subscribe|transfer|withdraw|authori[sz]e|allow|grant|donate|check ?out|add to (?:cart|bag|basket)|log ?out|log ?off|sign ?out|install|reset|revoke|erase|destroy|wipe|discard|deactivate|terminate|accept|agree|approve|continue|proceed|" +
      "senden|absenden|bezahlen|kaufen|löschen|bestellen|entfernen|envoyer|payer|acheter|supprimer|enviar|pagar|comprar|eliminar|borrar|verwijderen|betalen|kopen|удалить|оплатить|купить|" +
      "cancel\\s+(?:my\\s+)?subscription|close\\s+(?:my\\s+)?account";
    // Chinese, Japanese and Korean have no spaces between words: these are tested as substrings.
    const CJK = "削除|購入|支払い|支払う|删除|购买|支付|付款|삭제|결제|구매";
    const WORDS_RE = new RegExp("(?<![\\p{L}\\p{N}])(?:" + VERBS + ")(?![\\p{L}\\p{N}])|" + CJK, "iu");
    const WORDS = WORDS_RE;
    // Nouns that look like a verb at the start of a file or row name.
    const NOUNS = /\b(?:purchase\s+orders?|orders?\s+(?:numbers?|history|status|id|confirmation)|delete\s+key|post\s*(?:code|box|card)|payment\s+terms)\b/gi;
    // The words a target shows and the words it is named by, each judged on its own.
    const sayings = (el) => {
      const by = (el.getAttribute("aria-labelledby") || "").split(/\s+/).map((id) => (el.ownerDocument.getElementById(id) || {}).textContent || "");
      const alts = Array.from(el.querySelectorAll("img[alt]")).map((i) => i.getAttribute("alt"));
      return [el.getAttribute("aria-label"), ...by, el.textContent, el.value, el.getAttribute("title"), el.getAttribute("alt"), ...alts].filter(Boolean);
    };
    const said = (el) => sayings(el).join(" ");
    if (action === "consent") {
      // A box or a drop-down: not a click, but ticking or choosing may grant, accept or delete.
      // `extra` is the words around it. Only the verbs that grant or destroy, not "send" or "post".
      const ASK =
        "delete|remove|erase|destroy|wipe|discard|revoke|deactivate|terminate|reset|authori[sz]e|allow|grant|accept|agree|approve|proceed|consent|confirm|pay|buy|purchase|order|subscribe|unsubscribe|donate|publish|install|" +
        "löschen|bezahlen|kaufen|bestellen|akzeptieren|zustimmen|supprimer|payer|acheter|accepter|eliminar|pagar|comprar|aceptar";
      const ASKED = new RegExp("(?<![\\p{L}\\p{N}])(?:" + ASK + ")(?![\\p{L}\\p{N}])", "iu");
      return ASKED.test(said(this) + " " + (extra || "")) ? "submit_blocked" : "";
    }
    const list = [this];
    if (this.tagName === "LABEL" && this.control) list.push(this.control);
    for (const el of list) {
      const tag = el.tagName;
      const type = (el.getAttribute("type") || "").toLowerCase();
      if (action === "type") {
        if (tag === "INPUT" && ["password", "file", "hidden"].includes(type)) return "not_allowed";
        continue;
      }
      if (type === "submit" || (tag === "INPUT" && type === "image")) return "submit_blocked";
      // Through shadow roots: a form is also the host's, and a host may be a form-associated control.
      let inForm = !!el.form;
      let clickable = false;
      for (let n = el; n; n = n.parentElement || (n.getRootNode && n.getRootNode().host) || null) {
        if (n.tagName === "FORM" || n.form) inForm = true;
        if (n.matches && n.matches(CLICKABLE)) clickable = true;
      }
      if (inForm && clickable) return "submit_blocked";
      const ROWS = ["option", "row", "gridcell", "listitem", "treeitem"];
      const isRow = (n) => ROWS.includes(n.getAttribute("role")) && !n.hasAttribute("onclick") && n.tagName !== "A" && n.tagName !== "BUTTON";
      // The element, or one of its three nearest ancestors, that is clickable: that is what the click reaches.
      let host = null;
      for (let n = el, i = 0; n && n.matches && i < 4 && !host; n = n.parentElement, i++) if (n.matches(CLICKABLE)) host = n;
      let pointing = false;
      try {
        pointing = el.ownerDocument.defaultView.getComputedStyle(el).cursor === "pointer";
      } catch {
        // no style, no cursor
      }
      const words = (list) => list.some((t) => WORDS.test(t));
      if (host && isRow(host)) {
        // Rows of text (files, list items) are named by their words, not tested by them, except
        // for the words that destroy or buy once nouns such as "purchase order" are set aside.
        if (words(sayings(el).map((t) => t.replace(NOUNS, " ")))) return "submit_blocked";
      } else if ((host || pointing || el.hasAttribute("tabindex") || (el.textContent || "").trim().length <= 80) && words(sayings(el))) {
        // A listener cannot be seen from here, so a short bare text with such words is held too.
        return "submit_blocked";
      }
      if (tag === "A" && el.hasAttribute("href")) {
        const raw = (el.getAttribute("href") || "").replace(/[\u0000-\u0020\u007f-\u009f]/g, "");
        if (/^(?:javascript|data|vbscript):/i.test(raw)) return "submit_blocked";
        let where = raw;
        try {
          const u = new URL(el.href);
          where = u.pathname + " " + u.search;
          where = decodeURIComponent(where);
        } catch {
          // keep the raw text
        }
        if (WORDS.test(where.replace(/[\/_+=&?.-]+/g, " "))) return "submit_blocked";
      }
    }
    return "";
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

  const HELD = "that could submit or change something, so it was not clicked: press that yourself";

  /**
   * checkableOf: the box, radio or switch a name points at, and only when the
   * page ties them: the element itself (its own name), its label (for= or
   * wrapping), a box naming it in aria-labelledby, or a box standing beside
   * that one text in a row of its own. {box, unclear}: unclear is a box that
   * is merely near the words, which is not enough to tick it.
   */
  function checkableOf(el) {
    if (el.matches && el.matches(CHECKABLE)) return { box: el, unclear: false };
    if (el.tagName === "LABEL" && el.control && el.control.matches(CHECKABLE)) return { box: el.control, unclear: false };
    const inner = el.tagName === "LABEL" ? el.querySelector(CHECKABLE) : null;
    if (inner) return { box: inner, unclear: false };
    if (el.id) {
      const rootNode = el.getRootNode();
      const by = Array.from(rootNode.querySelectorAll ? rootNode.querySelectorAll("[aria-labelledby]") : []).filter(
        (b) => b.matches(CHECKABLE) && b.getAttribute("aria-labelledby").split(/\s+/).includes(el.id)
      );
      if (by.length === 1) return { box: by[0], unclear: false };
    }
    // A button or link is plainly not a box's text.
    const row = el.matches("button,a,input,select,textarea,[role=button],[role=link]") ? null : el.parentElement;
    if (!row) return { box: null, unclear: false };
    const boxes = row.querySelectorAll(CHECKABLE);
    if (boxes.length === 1) {
      const b = boxes[0];
      const beside = el.previousElementSibling === b || el.nextElementSibling === b;
      // The row is that box and that one text, nothing else to be mistaken for its label.
      if (beside && squash(row.textContent) === squash(el.textContent)) return { box: b, unclear: false };
      return { box: null, unclear: true };
    }
    return { box: null, unclear: boxes.length > 1 };
  }
  const checkable = (el) => checkableOf(el).box;

  const ownerAction = (action) => (action === "check" || action === "uncheck" ? "click" : action);

  /** wordsAround: what a control and the element the card named say, with its labels. */
  function wordsAround(control, named) {
    return wordsOf(control).names.concat(squash(named.textContent).slice(0, 200)).join(" ");
  }

  /**
   * find says what `target` matches in this frame, without doing anything:
   * {count, exact, matched, held, origin, href}. The bridge asks every frame, and only when
   * the answers add up to one match does it run the step, in that frame.
   */
  function find(action, target) {
    const kind = ownerAction(action);
    const found = resolve(root.document, target, kind === "select" || kind === "hover" || kind === "scroll" || kind === "press_key" ? "click" : kind);
    let held = "";
    if (found.count === 1) {
      const el = found.elements[0];
      if (action === "check" || action === "uncheck") {
        const c = checkableOf(el);
        held = c.box ? guard.call(c.box, "consent", wordsAround(c.box, el)) : c.unclear ? "ambiguous" : "not_found";
      } else if (action === "select") {
        held = guard.call(el, "consent", wordsAround(el, el));
      } else held = action === "hover" || action === "scroll" || action === "press_key" ? "" : guard.call(el, action);
    }
    const loc = root.location;
    return { count: found.count, exact: found.exact, matched: found.matched.slice(0, 200), held, origin: loc.origin, href: loc.href.split("#")[0] };
  }

  function setChecked(el, want) {
    const now = el.matches('input[type="checkbox"],input[type="radio"]') ? el.checked : el.getAttribute("aria-checked") === "true";
    if (now === want) return true;
    // A radio never unchecks itself: only another radio in its group can.
    if (!want && el.matches('input[type="radio"],[role="radio"]')) return false;
    press(el, 1);
    return true;
  }

  function pickOption(el, text) {
    const want = low(squash(text));
    const sel = el.tagName === "SELECT" ? el : el.querySelector("select");
    if (!sel) return false;
    const opt = Array.from(sel.options).find((o) => low(squash(o.textContent)) === want || low(squash(o.value)) === want);
    if (!opt || opt.disabled) return false;
    // Choosing fires input and change, which many pages treat as "go": the option's words count.
    if (guard.call(opt, "consent", opt.value)) return "submit_blocked";
    sel.focus();
    sel.value = opt.value;
    sel.dispatchEvent(new Event("input", { bubbles: true }));
    sel.dispatchEvent(new Event("change", { bubbles: true }));
    return true;
  }

  function scrollTo(el, how) {
    const w = String(how || "down").toLowerCase();
    const box = el || root.document.scrollingElement || root.document.documentElement;
    const page = el ? el.clientHeight : root.innerHeight;
    const by = w === "up" ? -page * 0.8 : w === "top" ? -1e9 : w === "bottom" ? 1e9 : page * 0.8;
    if (el) el.scrollBy({ top: by, behavior: "instant" });
    else root.scrollBy({ top: by, behavior: "instant" });
    return box;
  }

  function keyInto(el, key) {
    const t = el || root.document.activeElement || root.document.body;
    if (el && el.focus) el.focus();
    const init = { key, code: key, bubbles: true, cancelable: true, composed: true };
    t.dispatchEvent(new KeyboardEvent("keydown", init));
    t.dispatchEvent(new KeyboardEvent("keyup", init));
  }

  /** run does one approved action on the page; the result is plain data. origin: the origin the card was proposed on (the top frame checks it). */
  function run(action, target, text, origin, frameHref) {
    const changed = { ok: false, code: "page_changed", error: "the page changed since this was suggested" };
    if (typeof origin === "string" && root === root.top && root.location.origin !== origin) return changed;
    // A subframe: the document that was searched must still be the one acting.
    if (typeof frameHref === "string" && root !== root.top && root.location.href.split("#")[0] !== frameHref) return changed;
    const bare = (action === "scroll" || action === "press_key") && !squash(target);
    let el = null;
    let found = { count: 1, exact: true, matched: "" };
    if (!bare) {
      const f = find(action, target);
      if (f.count === 0) return { ok: false, code: "not_found", error: "not found" };
      if (f.count > 1) return { ok: false, code: "ambiguous", error: `ambiguous (${f.count} matches)` };
      if (f.held === "submit_blocked") return { ok: false, code: f.held, error: HELD };
      if (f.held === "ambiguous") return { ok: false, code: "ambiguous", error: "ambiguous: that text is not tied to a box (no label, for= or aria-labelledby)" };
      if (f.held === "not_found") return { ok: false, code: "not_found", error: "no box, radio or switch is named that" };
      if (f.held) return { ok: false, code: f.held, error: "that field is not one to type into" };
      found = f;
      el = resolve(root.document, target, ownerAction(action) === "type" ? "type" : "click").elements[0];
    }
    if (el) el.scrollIntoView({ block: "center", inline: "center" });
    if (action === "type") typeInto(el, String(text == null ? "" : text));
    else if (action === "double_click") {
      press(el, 1);
      press(el, 2);
      pointer(el, "dblclick", 2, { buttons: 0 });
    } else if (action === "click") press(el, 1);
    else if (action === "check" || action === "uncheck") {
      if (!setChecked(checkable(el), action === "check")) return { ok: false, code: "not_allowed", error: "a radio is unchecked by choosing another in its group" };
    } else if (action === "select") {
      const picked = pickOption(el, text);
      if (picked === "submit_blocked") return { ok: false, code: picked, error: HELD };
      if (!picked) return { ok: false, code: "not_found", error: "that option is not in the list" };
    } else if (action === "hover") {
      for (const t of ["pointerover", "mouseover", "pointerenter", "mouseenter", "pointermove", "mousemove"]) pointer(el, t, 0, { buttons: 0 });
    } else if (action === "scroll") scrollTo(el, text);
    else if (action === "press_key") {
      if (!KEYS.has(String(text))) return { ok: false, code: "not_allowed", error: "that key is not one to press" };
      keyInto(el, String(text));
    } else return { ok: false, code: "invalid", error: "unknown action" };
    const done = { ok: true, matched: found.matched.slice(0, 200), exact: found.exact };
    if (root !== root.top) done.frameOrigin = root.location.origin;
    return done;
  }

  root.MonoPageAction = { run, find, resolve, guard, bounded, HELD, ACTIONABLE, KEYS };
})(globalThis);
