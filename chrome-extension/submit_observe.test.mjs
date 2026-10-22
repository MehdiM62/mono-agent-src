// Tests for submit_observe.js: the worker half against a fake chrome, the
// page half against a fake window/DOM.
// `node --test 'chrome-extension/**/*.test.mjs'`.

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const settle = () => new Promise((r) => setTimeout(r, 10));

/** A browser with one agent-grouped tab (7), one ordinary tab (8) and a bridge that records requests. */
function setup({ supported = true } = {}) {
  const listeners = { message: [], committed: [], history: [], removed: [] };
  const asked = [];
  const injected = [];
  const pageSent = []; // what the page half sends with chrome.runtime.sendMessage
  const tabs = {
    7: { id: 7, groupId: 1, url: "https://jobs.example/apply" },
    8: { id: 8, groupId: -1, url: "https://jobs.example/apply" },
  };
  const chrome = {
    runtime: { id: "ext-id", sendMessage: (m) => pageSent.push(m), onMessage: { addListener: (fn) => listeners.message.push(fn) } },
    webNavigation: {
      onCommitted: { addListener: (fn) => listeners.committed.push(fn) },
      onHistoryStateUpdated: { addListener: (fn) => listeners.history.push(fn) },
    },
    tabs: { onRemoved: { addListener: (fn) => listeners.removed.push(fn) }, get: async (id) => tabs[id] },
    tabGroups: { get: async (id) => ({ id, title: id === 1 ? "Coder · Fill" : "Work" }) },
    scripting: { executeScript: async (o) => (injected.push(o), [{ result: true }]) },
  };
  const MonoAsk = {
    supports: async (m) => supported && m === "form.submit_observed",
    request: async (method, params) => (asked.push({ method, params }), { matched: true }),
  };
  const g = loadExtensionScripts(["submit_observe.js"], { chrome, MonoAsk });
  g.MonoAsk = MonoAsk;
  g.MonoSubmitObserve.install();
  const S = g.MonoSubmitObserve;
  const send = (stage, tabId = 7, extra = {}) =>
    listeners.message.forEach((fn) => fn({ type: S.MESSAGE, stage }, { id: "ext-id", tab: tabs[tabId], frameId: 0, ...extra }));
  const commit = async (tabId, url, extra = {}) => {
    listeners.committed.forEach((fn) => fn({ tabId, frameId: 0, url, transitionType: "form_submit", ...extra }));
    await settle();
  };
  return { S, asked, injected, pageSent, tabs, send, commit, listeners, history: async (tabId, url, frameId = 0) => (listeners.history.forEach((fn) => fn({ tabId, frameId, url })), await settle()) };
}

test("a trusted submit followed by a navigation is reported once, with no query or fragment", async () => {
  const t = setup();
  t.send("submit");
  await t.commit(7, "https://jobs.example/thanks?token=abc#frag");
  assert.deepEqual(t.asked, [{ method: "form.submit_observed", params: { tab_id: 7, origin: "https://jobs.example", url: "https://jobs.example/thanks" } }]);
  await t.commit(7, "https://jobs.example/other");
  assert.equal(t.asked.length, 1, "one submit is reported once");
});

test("a trusted submit followed by the form going away is reported", async () => {
  const t = setup();
  t.send("submit");
  t.send("form_changed");
  await settle();
  assert.equal(t.asked.length, 1);
  assert.equal(t.asked[0].params.tab_id, 7);
});

test("an SPA's history change after a trusted submit is reported", async () => {
  const t = setup();
  t.send("submit");
  await t.history(7, "https://jobs.example/done");
  assert.equal(t.asked.length, 1);
});

test("nothing is reported without a trusted submit: a page that says 'submitted', a bare navigation, a bare form change", async () => {
  const t = setup();
  await t.commit(7, "https://jobs.example/thanks"); // 'Application submitted' page, no submit event
  t.send("form_changed");
  await t.history(7, "https://jobs.example/spa");
  await settle();
  assert.deepEqual(t.asked, []);
});

test("a trusted submit that leads nowhere reports nothing; a late navigation is not its doing", async () => {
  const t = setup();
  t.send("submit");
  await settle();
  assert.deepEqual(t.asked, []);
  const realNow = Date.now;
  Date.now = () => realNow() + t.S.WINDOW_MS + 1000;
  try {
    await t.commit(7, "https://jobs.example/later");
  } finally {
    Date.now = realNow;
  }
  assert.deepEqual(t.asked, [], "the window passed");
});

test("a reload after a submit is not a confirmation", async () => {
  const t = setup();
  t.send("submit");
  await t.commit(7, "https://jobs.example/apply", { transitionType: "reload" });
  assert.deepEqual(t.asked, []);
});

test("only the top frame of the extension's own pages counts, and the origin is the tab's, not the message's", async () => {
  const t = setup();
  t.send("submit", 7, { frameId: 3 }); // a subframe
  t.send("submit", 7, { id: "other-ext" });
  t.listeners.message.forEach((fn) => fn({ type: t.S.MESSAGE, stage: "submit", origin: "https://evil.example" }, { id: "ext-id", tab: undefined, frameId: 0 }));
  await t.commit(7, "https://jobs.example/thanks");
  assert.deepEqual(t.asked, []);
  t.send("submit"); // a real one, whose message tries to name another origin
  t.listeners.message.forEach((fn) => fn({ type: t.S.MESSAGE, stage: "submit", origin: "https://evil.example" }, { id: "ext-id", tab: t.tabs[7], frameId: 0 }));
  await t.commit(7, "https://jobs.example/thanks");
  assert.equal(t.asked[0].params.origin, "https://jobs.example");
});

test("an older bridge without the method gets nothing", async () => {
  const t = setup({ supported: false });
  t.send("submit");
  await t.commit(7, "https://jobs.example/thanks");
  assert.deepEqual(t.asked, []);
});

test("the observer is injected only into tabs of an agent group, in the top frame", async () => {
  const t = setup();
  await t.commit(7, "https://jobs.example/apply", { transitionType: "link" });
  await t.commit(8, "https://jobs.example/apply", { transitionType: "link" });
  assert.equal(t.injected.length, 1);
  assert.deepEqual(t.injected[0].target, { tabId: 7, frameIds: [0] });
  assert.equal(t.injected[0].func, t.S.installObserver);
  await t.commit(7, "chrome://settings", { transitionType: "link" });
  assert.equal(t.injected.length, 1, "a non-web page is not instrumented");
});

// --- the page half ----------------------------------------------------------

/** A window with just enough DOM: addEventListener, a form, a MutationObserver. */
function pageWorld() {
  const handlers = [];
  class Form {
    constructor() { this.isConnected = true; this.hidden = false; this.style = { display: "block", visibility: "visible" }; }
  }
  const observers = [];
  class MO {
    constructor(fn) { this.fn = fn; observers.push(this); }
    observe() {}
    disconnect() { this.off = true; }
  }
  const win = {
    addEventListener: (type, fn, capture) => handlers.push({ type, fn, capture }),
  };
  const globals = {
    window: win,
    HTMLFormElement: Form,
    MutationObserver: MO,
    getComputedStyle: (f) => f.style,
    document: { documentElement: {} },
    chrome: { runtime: { sendMessage: (m) => sent.push(m) } },
  };
  return { win, handlers, Form, observers, globals };
}

function withGlobals(globals, fn) {
  const saved = {};
  for (const k of Object.keys(globals)) {
    saved[k] = Object.getOwnPropertyDescriptor(globalThis, k);
    Object.defineProperty(globalThis, k, { value: globals[k], configurable: true, writable: true });
  }
  try {
    return fn();
  } finally {
    for (const k of Object.keys(globals)) {
      if (saved[k]) Object.defineProperty(globalThis, k, saved[k]);
      else delete globalThis[k];
    }
  }
}

test("the page half reports only trusted submit events of forms, and a form that goes away", () => {
  const { S, pageSent } = setup();
  const w = pageWorld();
  withGlobals(w.globals, () => {
    assert.equal(S.installObserver("__k", S.MESSAGE, 1000), true);
    assert.equal(S.installObserver("__k", S.MESSAGE, 1000), true);
    const submits = w.handlers.filter((h) => h.type === "submit");
    assert.equal(submits.length, 1, "installed once");
    assert.equal(submits[0].capture, true);
    const form = new w.Form();
    submits[0].fn({ isTrusted: false, target: form }); // page script's submit
    submits[0].fn({ isTrusted: true, target: {} }); // not a form
    assert.deepEqual(pageSent, []);
    submits[0].fn({ isTrusted: true, target: form });
    assert.deepEqual(pageSent, [{ type: S.MESSAGE, stage: "submit" }]);
    // The form is still there: a change elsewhere reports nothing.
    w.observers.forEach((o) => o.fn());
    assert.equal(pageSent.length, 1);
    form.isConnected = false; // confirmation replaced the form
    w.observers.forEach((o) => o.fn());
    assert.deepEqual(pageSent.at(-1), { type: S.MESSAGE, stage: "form_changed" });
  });
});
