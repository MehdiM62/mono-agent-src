/**
 * MonoAgent Bridge - the floating panel's shell, in the page (RCL-04, tasks)
 *
 * The small dark panel the highlighter offers beside a selection, and the one
 * it shows on an existing highlight. Its own file because it is the part a
 * page must not be able to drive, and because highlight_page.js is long
 * enough.
 *
 * A page shares the DOM with this content script. A panel in the page's own
 * DOM can be found with querySelector and clicked from the page's script,
 * which, once the panel can add a task, is a web page writing into the
 * person's task board. So:
 *
 *   - the panel lives in a CLOSED shadow root: to the page the host has no
 *     shadowRoot and no children, so its buttons cannot be found or
 *     restyled from the page;
 *   - every handler on it runs only for an event the browser marks
 *     isTrusted: an event a script dispatched is ignored, and so is one
 *     that carries no isTrusted at all.
 *
 * What this does not stop: the host itself is in the page's DOM, so a page
 * can move or cover it and bait a real click from the person. Such a click
 * adds an Inbox task with text the page chose, which the operator reads
 * before approving it (the gate, spec D6).
 *
 * Loaded before highlight_page.js (manifest.json), into the same isolated
 * world. Node-tested against a fake document (highlight_panel.test.mjs).
 */

(function (root) {
  "use strict";

  const UI_ID = "monoagent-highlight-ui";

  /** trusted wraps a listener so that only a real user event reaches it. */
  function trusted(fn) {
    return function (event) {
      if (!event || event.isTrusted !== true) return undefined;
      return fn.call(this, event);
    };
  }

  /** close removes the panel, if one is open. */
  function close(doc) {
    const existing = doc.getElementById(UI_ID);
    if (existing) existing.remove();
  }

  /**
   * open shows an empty panel at (x, y) in page coordinates and returns the
   * box to put its controls in. The host is the only node the page can see.
   */
  function open(doc, x, y) {
    close(doc);
    const host = doc.createElement("div");
    host.id = UI_ID;
    host.style.cssText = ["all:initial", "position:absolute", `left:${Math.round(x)}px`, `top:${Math.round(y)}px`, "z-index:2147483647"].join(";");
    // A press inside must not reach the page, or the highlighter's own "a
    // press outside closes it". This may run for any event: all it does is
    // stop one.
    host.addEventListener("mousedown", (e) => e.stopPropagation());
    const shadow = host.attachShadow({ mode: "closed" });
    const box = doc.createElement("div");
    box.style.cssText = [
      "background:#0b0f14",
      "color:#e2e8f0",
      "font:13px/1.4 -apple-system,system-ui,sans-serif",
      "border-radius:8px",
      "padding:6px",
      "box-shadow:0 6px 24px rgba(0,0,0,.4)",
      "display:flex",
      "gap:6px",
      "align-items:center",
    ].join(";");
    shadow.appendChild(box);
    doc.body.appendChild(host);
    addCoderButton(doc, box);
    return box;
  }

  /** button is one of the panel's buttons; only a real click runs onClick. */
  function button(doc, label, title, onClick) {
    const el = doc.createElement("button");
    el.textContent = label;
    el.title = title || label;
    el.style.cssText = "background:#1e293b;color:#e2e8f0;border:0;border-radius:6px;padding:4px 8px;cursor:pointer;font:inherit";
    el.addEventListener("click", trusted(onClick));
    return el;
  }

  /** capBytes cuts s to at most max bytes of UTF-8, never inside a character. */
  function capBytes(s, max) {
    const text = String(s == null ? "" : s);
    const bytes = new TextEncoder().encode(text);
    if (bytes.length <= max) return text;
    let end = max;
    while (end > 0 && (bytes[end] & 0xc0) === 0x80) end--;
    return new TextDecoder().decode(bytes.subarray(0, end));
  }

  // ── The element a coder request is about (coder_bridge.js) ─────────
  //
  // "Send this element to coder" in the right-click menu, and the panel's own
  // coder button, hand the worker an element, never text: the worker reads
  // it back with chrome.scripting, in this same isolated world, and turns it
  // into a selector. Only a real event sets one; a page that fires its own
  // contextmenu or mouseup changes nothing. A highlight's <mark> is ours, so
  // the element it sits in is the one named.

  const MARK = "mark.monoagent-highlight";
  const targets = { context: null, panel: null, pointer: null };

  function elementOf(node) {
    let n = node;
    while (n && n.nodeType !== 1) n = n.parentNode;
    if (!n || n.id === UI_ID) return null;
    const mark = n.closest ? n.closest(MARK) : null;
    return (mark ? mark.parentElement : n) || null;
  }

  function selectionElement(doc) {
    const selection = doc.getSelection ? doc.getSelection() : null;
    if (!selection || selection.isCollapsed || !selection.rangeCount) return null;
    return elementOf(selection.getRangeAt(0).commonAncestorContainer);
  }

  /** remembered is the element last right-clicked ("context") or the one the open panel is beside ("panel"). */
  function remembered(which) {
    return which === "context" || which === "panel" ? targets[which] : null;
  }

  /**
   * addCoderButton puts "→ coder" last in an opened panel: after the
   * caller's own buttons, which are appended once open() returns.
   */
  function addCoderButton(doc, box) {
    targets.panel = selectionElement(doc) || targets.pointer;
    const ext = root.chrome && root.chrome.runtime;
    if (!ext || !ext.sendMessage) return;
    queueMicrotask(() => {
      if (doc.getElementById(UI_ID) === null) return; // closed already
      const coder = button(doc, "→ coder", "Send this element to the coder (opens MonoAgent's side panel)", () => {
        close(doc);
        try {
          ext.sendMessage({ type: "coder_from_page" }, () => void root.chrome.runtime.lastError);
        } catch {
          // the extension was reloaded under this page: nothing to tell
        }
      });
      box.appendChild(coder);
    });
  }

  if (root.document && typeof root.document.addEventListener === "function") {
    root.document.addEventListener("contextmenu", trusted((e) => (targets.context = elementOf(e.target))), true);
    root.document.addEventListener("mouseup", trusted((e) => (targets.pointer = elementOf(e.target))), true);
  }

  root.MonoHighlightPanel = { UI_ID, trusted, open, close, button, capBytes, remembered };
})(globalThis);
