// The highlight panel's coder half (highlight_panel.js): the "→ coder"
// button, and the element a coder request names, remembered from real
// events only. Against a fake document; the worker's half, which reads the
// element back, is coder_bridge.test.mjs.
// `CHROME_PATH=/nonexistent node --test chrome-extension/highlight_coder.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const flush = () => new Promise((r) => setTimeout(r, 0));

function fakeDom() {
  const byId = new Map();
  const docListeners = {};
  function element(tag, parent = null) {
    const el = {
      tagName: tag.toUpperCase(), nodeType: 1, id: "", title: "", textContent: "", style: {}, children: [],
      parentNode: parent, parentElement: parent, listeners: {}, attached: null,
      addEventListener(type, fn) { (this.listeners[type] = this.listeners[type] || []).push(fn); },
      appendChild(child) { child.parentNode = this; child.parentElement = this; this.children.push(child); if (child.id) byId.set(child.id, child); return child; },
      remove() { if (this.parentNode) this.parentNode.children = this.parentNode.children.filter((c) => c !== this); this.parentNode = null; if (byId.get(this.id) === this) byId.delete(this.id); },
      attachShadow() { const shadow = { children: [], appendChild(c) { this.children.push(c); return c; } }; this.attached = shadow; return shadow; },
      closest(sel) { for (let n = this; n; n = n.parentElement) if (sel === "mark.monoagent-highlight" && n.tagName === "MARK" && n.className === "monoagent-highlight") return n; return null; },
      fire(type, event) { for (const fn of this.listeners[type] || []) fn.call(this, event); },
    };
    return el;
  }
  const body = element("body");
  let selection = null;
  const doc = {
    body,
    createElement: (t) => element(t),
    getElementById: (id) => byId.get(id) || null,
    getSelection: () => selection,
    addEventListener: (type, fn, capture) => (docListeners[type] = docListeners[type] || []).push({ fn, capture }),
    fire: (type, event) => (docListeners[type] || []).forEach((l) => l.fn(event)),
  };
  return { doc, body, element, docListeners, select: (s) => (selection = s) };
}

function setup() {
  const dom = fakeDom();
  const sent = [];
  const chrome = { runtime: { lastError: undefined, sendMessage: (m, cb) => { sent.push(m); if (cb) cb(); } } };
  const { MonoHighlightPanel: UI } = loadExtensionScripts(["highlight_panel.js"], { document: dom.doc, chrome });
  return { UI, sent, ...dom };
}

test("the right-click and mouseup listeners are capture-phase, so a page cannot stop them first", () => {
  const t = setup();
  assert.equal(t.docListeners.contextmenu.length, 1);
  assert.equal(t.docListeners.contextmenu[0].capture, true);
  assert.equal(t.docListeners.mouseup[0].capture, true);
});

test("only a real right-click names the element; a <mark> of ours names the element it sits in", () => {
  const t = setup();
  const p = t.element("p", t.body);
  const mark = t.element("mark", p);
  mark.className = "monoagent-highlight";
  const text = { nodeType: 3, parentNode: mark };
  t.doc.fire("contextmenu", { isTrusted: false, target: p });
  assert.equal(t.UI.remembered("context"), null, "the page named an element");
  t.doc.fire("contextmenu", { isTrusted: true, target: text });
  assert.equal(t.UI.remembered("context"), p);
  assert.equal(t.UI.remembered("pointer"), null, "only context and panel are offered");
});

test("the panel names the selection's element, or else what the click landed on", () => {
  const t = setup();
  const article = t.element("article", t.body);
  const para = t.element("p", article);
  t.select({ isCollapsed: false, rangeCount: 1, getRangeAt: () => ({ commonAncestorContainer: { nodeType: 3, parentNode: para } }) });
  t.UI.open(t.doc, 0, 0);
  assert.equal(t.UI.remembered("panel"), para);
  t.select({ isCollapsed: true, rangeCount: 0 });
  t.doc.fire("mouseup", { isTrusted: true, target: article });
  t.UI.open(t.doc, 0, 0);
  assert.equal(t.UI.remembered("panel"), article);
});

test("→ coder comes after the caller's buttons and sends only its type, on a real click", async () => {
  const t = setup();
  const box = t.UI.open(t.doc, 0, 0);
  box.appendChild(t.UI.button(t.doc, "＋ task", "Add the selection as a task", () => {}));
  await flush();
  const labels = box.children.map((b) => b.textContent);
  assert.deepEqual(labels, ["＋ task", "→ coder"]);
  const coder = box.children[1];
  coder.fire("click", { isTrusted: false });
  assert.equal(t.sent.length, 0, "the page pressed the button");
  coder.fire("click", { isTrusted: true });
  assert.deepEqual(t.sent, [{ type: "coder_from_page" }]);
  assert.equal(t.doc.getElementById(t.UI.UI_ID), null, "the panel closes");
});

test("a panel closed before the button was added gets none", async () => {
  const t = setup();
  const box = t.UI.open(t.doc, 0, 0);
  t.UI.close(t.doc);
  await flush();
  assert.equal(box.children.length, 0);
});
