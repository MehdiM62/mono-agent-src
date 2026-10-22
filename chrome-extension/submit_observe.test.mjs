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

test("an SPA's history change after a trusted submit is reported", async () => {
  const t = setup();
  t.send("submit");
  await t.history(7, "https://jobs.example/done");
  assert.equal(t.asked.length, 1);
});

test("nothing is reported without a trusted submit: a page that says 'submitted', a bare navigation, a bare form change", async () => {
  const t = setup();
  await t.commit(7, "https://jobs.example/thanks"); // 'Application submitted' page, no submit event
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

/** A window with just enough DOM: listeners by type, a form with a submit button. */
function pageWorld() {
  const handlers = {};
  class Form {
    constructor() {
      this.inputs = [];
    }
    contains(el) {
      return el === this || this.inputs.includes(el);
    }
  }
  const mk = (tag, type, form) => {
    const el = { tagName: tag, form, getAttribute: (n) => (n === "type" ? type : null), closest: () => el };
    return el;
  };
  const win = { addEventListener: (type, fn) => (handlers[type] ||= []).push(fn) };
  const globals = { window: win, HTMLFormElement: Form };
  const fire = (type, ev) => (handlers[type] || []).forEach((fn) => fn(ev));
  return { handlers, Form, globals, fire, mk };
}

async function withGlobals(globals, fn) {
  const saved = {};
  for (const k of Object.keys(globals)) {
    saved[k] = Object.getOwnPropertyDescriptor(globalThis, k);
    Object.defineProperty(globalThis, k, { value: globals[k], configurable: true, writable: true });
  }
  try {
    return await fn();
  } finally {
    for (const k of Object.keys(globals)) {
      if (saved[k]) Object.defineProperty(globalThis, k, saved[k]);
      else delete globalThis[k];
    }
  }
}

/** world installs the observer and returns helpers; `sent` is what the page sent the worker. */
function observed(body) {
  const { S, pageSent } = setup();
  const w = pageWorld();
  return withGlobals(w.globals, async () => {
    assert.equal(S.installObserver("__k", S.MESSAGE), true);
    assert.equal(S.installObserver("__k", S.MESSAGE), true);
    assert.equal(w.handlers.submit.length, 1, "installed once");
    const form = new w.Form();
    const button = w.mk("BUTTON", "submit", form);
    const field = { tagName: "INPUT" };
    form.inputs.push(field);
    await body({ S, w, form, button, field, sent: pageSent });
  });
}

const tick = () => new Promise((r) => setTimeout(r, 5));

test("a scripted requestSubmit with no gesture reports nothing, though its event is trusted", () =>
  observed(async ({ w, form, sent }) => {
    w.fire("submit", { isTrusted: true, target: form, submitter: null, defaultPrevented: false });
    await tick();
    assert.deepEqual(sent, []);
  }));

test("a gesture on the page but not on this form's submit control does not count", () =>
  observed(async ({ w, form, button, sent }) => {
    w.fire("click", { isTrusted: true, target: { tagName: "DIV", closest: () => null } }); // the person clicked elsewhere
    w.fire("submit", { isTrusted: true, target: form, submitter: null, defaultPrevented: false });
    w.fire("click", { isTrusted: false, target: button }); // a script's click
    w.fire("submit", { isTrusted: true, target: form, submitter: button, defaultPrevented: false });
    w.fire("click", { isTrusted: true, target: w.mk("BUTTON", "submit", new w.Form()) }); // another form's button
    w.fire("submit", { isTrusted: true, target: form, submitter: button, defaultPrevented: false });
    await tick();
    assert.deepEqual(sent, []);
  }));

test("a submit the page cancelled reports nothing (preventDefault, then hide the form)", () =>
  observed(async ({ w, form, button, sent }) => {
    w.fire("click", { isTrusted: true, target: button });
    const ev = { isTrusted: true, target: form, submitter: button, defaultPrevented: false };
    w.fire("submit", ev);
    ev.defaultPrevented = true; // the page's handler ran after ours and cancelled it
    await tick();
    assert.deepEqual(sent, []);
  }));

test("the genuine path: a click on the submit control, an uncancelled submit", () =>
  observed(async ({ S, w, form, button, sent }) => {
    w.fire("click", { isTrusted: true, target: button });
    w.fire("submit", { isTrusted: true, target: form, submitter: button, defaultPrevented: false });
    await tick();
    assert.deepEqual(sent, [{ type: S.MESSAGE, stage: "submit" }]);
  }));

test("Enter inside a field of the form counts; Enter in a textarea or outside does not", () =>
  observed(async ({ S, w, form, field, sent }) => {
    w.fire("keydown", { isTrusted: true, key: "Enter", target: { tagName: "TEXTAREA" } });
    w.fire("submit", { isTrusted: true, target: form, submitter: null, defaultPrevented: false });
    w.fire("keydown", { isTrusted: true, key: "Enter", target: {} });
    w.fire("submit", { isTrusted: true, target: form, submitter: null, defaultPrevented: false });
    await tick();
    assert.deepEqual(sent, []);
    w.fire("keydown", { isTrusted: true, key: "Enter", target: field });
    w.fire("submit", { isTrusted: true, target: form, submitter: null, defaultPrevented: false });
    await tick();
    assert.equal(sent.length, 1);
  }));

test("a gesture is used once and goes stale", () =>
  observed(async ({ S, w, form, button, sent }) => {
    w.fire("click", { isTrusted: true, target: button });
    w.fire("submit", { isTrusted: true, target: form, submitter: button, defaultPrevented: false });
    w.fire("submit", { isTrusted: true, target: form, submitter: button, defaultPrevented: false });
    w.fire("click", { isTrusted: true, target: button });
    const real = Date.now;
    Date.now = () => real() + 4000;
    try {
      w.fire("submit", { isTrusted: true, target: form, submitter: button, defaultPrevented: false });
    } finally {
      Date.now = real;
    }
    await tick();
    assert.equal(sent.length, 1);
  }));

test("a real click followed by the page's own requestSubmit() (no submitter) does not count", () =>
  observed(async ({ w, form, button, sent }) => {
    w.fire("click", { isTrusted: true, target: button });
    w.fire("submit", { isTrusted: true, target: form, submitter: null, defaultPrevented: false });
    await tick();
    assert.deepEqual(sent, []);
  }));
