// Tests for the recorder's Chrome wiring (recorder_wiring.js) against a fake
// `chrome`: messages from the page and the panel reach the session, the
// panel's port closing stops the recording, webNavigation is routed, and
// analyze / verify / save go out as extension→Go requests over ask.js.

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

function event() {
  let fns = [];
  return {
    addListener: (fn) => fns.push(fn),
    removeListener: (fn) => (fns = fns.filter((f) => f !== fn)),
    hasListener: (fn) => fns.includes(fn),
    fire: (...a) => fns.map((fn) => fn(...a)),
    get fns() {
      return fns;
    },
  };
}

function area(data = {}) {
  return {
    data,
    get: async (keys) => Object.fromEntries([].concat(keys).filter((k) => k in data).map((k) => [k, data[k]])),
    set: async (v) => Object.assign(data, JSON.parse(JSON.stringify(v))),
    remove: async (keys) => [].concat(keys).forEach((k) => delete data[k]),
  };
}

// Who is asking: the side panel (an extension page) or a page recorder (a tab).
const PANEL = { id: "ext", url: "chrome-extension://ext/sidepanel.html" };
// The id a side panel document makes for itself (crypto.randomUUID): in its
// port's name, and in its record_start.
const PANEL_ID = "2f1c9d4e-7b3a-4c5d-9e8f-0a1b2c3d4e5f";
const PAGE = (tabId) => ({ id: "ext", tab: { id: tabId }, url: "https://app.test/" });

function fakeChrome() {
  const data = {};
  const scripts = [];
  const ports = [];
  const c = {
    data,
    scripts,
    ports,
    runtime: {
      id: "ext",
      getURL: (p) => `chrome-extension://ext/${p}`,
      onMessage: event(),
      onConnect: event(),
      onStartup: event(),
    },
    tabs: {
      onCreated: event(),
      onRemoved: event(),
      get: async (id) => ({ id, url: `https://app.test/${id}`, title: "App" }),
      query: async () => [{ id: 7 }],
    },
    webNavigation: {
      onCommitted: event(),
      onHistoryStateUpdated: event(),
      onDOMContentLoaded: event(),
      onCreatedNavigationTarget: event(),
      onReferenceFragmentUpdated: event(),
    },
    scripting: { executeScript: async (spec) => (scripts.push(spec), [{}]) },
    action: {
      setBadgeText: async () => {},
      setBadgeBackgroundColor: async () => {},
      setTitle: async () => {},
    },
    storage: { local: area(data), session: area() },
  };
  return c;
}

// The side panel connects its port as it loads, before anyone can press
// Record; load() does the same unless a test is about a panel that is gone.
function load({ panel = true } = {}) {
  const chrome = fakeChrome();
  const wire = [];
  const g = loadExtensionScripts(["ask.js", "recorder_privacy.js", "recorder_outbox.js", "recorder_session.js", "recorder_wiring.js"], { chrome });
  const send = (f) => (wire.push(f), true);
  g.MonoAsk.install({ send, isConnected: () => true });
  g.MonoRecorderWiring.install({ send, isConnected: () => true, storage: chrome.storage.local });
  // A start names this panel unless a test says otherwise.
  const named = (msg) => (msg.type === "record_start" && !("panel" in msg) ? { ...msg, panel: PANEL_ID } : msg);
  const call = (msg, sender = PANEL) =>
    new Promise((resolve) => {
      const handled = chrome.runtime.onMessage.fire(named(msg), sender, resolve);
      if (!handled.some(Boolean)) resolve(undefined);
    });
  const port = (id = PANEL_ID) => {
    const p = { name: `${g.MonoRecorderWiring.PORT_NAME}:${id}`, sender: PANEL, onMessage: event(), onDisconnect: event(), sent: [] };
    p.postMessage = (m) => p.sent.push(m);
    chrome.runtime.onConnect.fire(p);
    return p;
  };
  const settle = () => new Promise((r) => setTimeout(r, 200));
  if (panel) port();
  return { g, chrome, wire, call, port, settle };
}

test("start injects the recorder files into every frame of the tab", async () => {
  const { chrome, wire, call } = load();
  chrome.data.captureProfile = "work";
  const res = await call({ type: "record_start", goal: "demo" });
  assert.equal(res.ok, true, res.error);
  assert.equal(res.state.tabId, 7, "defaults to the active tab");
  assert.deepEqual(chrome.scripts[0].files, [
    "recorder_privacy.js", "recorder_selectors.js", "recorder_list.js", "recorder_dom.js", "recorder.js", "recorder_network.js",
  ]);
  assert.deepEqual(chrome.scripts[0].target, { tabId: 7, allFrames: true });
  // The fetch/XHR hook goes into the page's own world, then is switched on.
  const main = chrome.scripts.filter((s) => s.world === "MAIN");
  assert.deepEqual(main[0].files, ["recorder_network.js"]);
  assert.equal(typeof main[1].func, "function");
  assert.equal(wire[0].op, "start");
});

test("captured API calls from the recorded tab become network frames", async () => {
  const { wire, call, settle } = load();
  await call({ type: "record_start", tabId: 7 });
  const entry = { method: "GET", url: "https://api.test/me", status: 200, contentType: "application/json" };
  await call({ type: "recorder_net", entry }, PAGE(7));
  await call({ type: "recorder_net", entry }, PAGE(8));
  await settle();
  const net = wire.filter((f) => f.op === "network");
  assert.equal(net.length, 1, "only the recorded tab's calls");
  assert.equal(net[0].net.url, "https://api.test/me");
});

test("stopping switches the page hook off", async () => {
  const { chrome, call } = load();
  await call({ type: "record_start", tabId: 7 });
  await call({ type: "record_stop" });
  const last = chrome.scripts.filter((s) => s.world === "MAIN").at(-1);
  assert.ok(String(last.func).includes("unhook"));
});

test("page events, navigation and new documents are routed to the session", async () => {
  const { chrome, wire, call, settle } = load();
  await call({ type: "record_start", tabId: 7 });
  await call({ type: "recorder_event", event: { type: "click", url: "https://app.test/7", at: Date.now() } }, PAGE(7));
  chrome.webNavigation.onCommitted.fire({ tabId: 7, frameId: 0, url: "https://app.test/b", transitionType: "link", transitionQualifiers: [] });
  chrome.webNavigation.onDOMContentLoaded.fire({ tabId: 7, frameId: 2 });
  await settle();
  const types = wire.filter((f) => f.op === "event").map((f) => f.event.type);
  assert.deepEqual(types, ["click", "navigated"]);
  assert.deepEqual(chrome.scripts.at(-1).target, { tabId: 7, frameIds: [2] }, "a frame that loaded later gets the recorder too");
});

test("closing the panel that started the recording stops it", async () => {
  const { wire, call, port, settle } = load();
  const p = port();
  await settle();
  assert.equal(p.sent[0].type, "record_state", "a new panel gets the state once the worker has restored it");
  p.onMessage.fire({ type: "record_owner" });
  await call({ type: "record_start", tabId: 7 });
  p.onDisconnect.fire();
  await settle();
  assert.deepEqual([wire.at(-1).op, wire.at(-1).reason], ["stop", "panel_closed"]);
});

test("a new tab from the recorded tab stops the recording", async () => {
  const { chrome, wire, call, settle } = load();
  await call({ type: "record_start", tabId: 7 });
  chrome.tabs.onCreated.fire({ id: 30, openerTabId: 7 });
  await settle();
  assert.equal(wire.at(-1).reason, "new_tab");
});

test("analyzing a recording this worker no longer knows still names the chosen profile", async () => {
  const { g, chrome, wire, call, settle } = load();
  chrome.data.captureProfile = "work";
  const analyzing = call({ type: "record_analyze", recordingId: "rec-from-before-a-restart" });
  await settle();
  const req = wire.find((f) => f.kind === "request" && f.method === "record.analyze");
  assert.ok(req, "record.analyze was asked");
  assert.deepEqual(req.params, { recordingId: "rec-from-before-a-restart", profile: "work" });
  g.MonoAsk.handleFrame({ kind: "reply", id: req.id, ok: true, data: { draftDir: "/d" } });
  assert.equal((await analyzing).ok, true);
});

test("analyze, verify and save are record.* requests to Go", async () => {
  const { g, chrome, wire, call, settle } = load();
  chrome.data.captureProfile = "work";
  await call({ type: "record_start", tabId: 7 });
  await call({ type: "record_stop" });
  const recordingId = wire[0].recordingId;
  // Go acks every frame; the stop ack says the envelope is written.
  for (const f of wire.filter((x) => x.kind === "recording")) {
    g.MonoRecorderWiring.handleFrame({ id: f.id, success: true, type: "recording" });
  }

  const answer = (method, data) => {
    const req = wire.find((f) => f.kind === "request" && f.method === method);
    assert.ok(req, `${method} was asked`);
    g.MonoAsk.handleFrame({ kind: "reply", id: req.id, ok: true, data });
    return req;
  };

  const analyzing = call({ type: "record_analyze" });
  await settle();
  const req = answer("record.analyze", { draftDir: "/d", draft: { action: "x" } });
  assert.deepEqual(req.params, { recordingId, profile: "work" }, "analyze looks in the profile's inbox");
  assert.deepEqual((await analyzing).result, { draftDir: "/d", draft: { action: "x" } });

  const verifying = call({ type: "record_verify", draftDir: "/d", inputs: { password: "pw-for-this-run" } });
  await settle();
  assert.deepEqual(answer("record.verify", { ok: true, steps: [] }).params, {
    draftDir: "/d",
    inputs: { password: "pw-for-this-run" },
    profile: "work",
  });
  assert.equal((await verifying).ok, true);

  const saving = call({ type: "record_save", draftDir: "/d", saveAs: "fragment", automation: "crm", name: "login" });
  await settle();
  assert.deepEqual(answer("record.save", { automation: "crm" }).params, {
    draftDir: "/d",
    saveAs: "fragment",
    automation: "crm",
    name: "login",
  });
  assert.equal((await saving).ok, true);

  const creating = call({ type: "record_save", draftDir: "/e", automation: "newcrm", isNew: true });
  await settle();
  const req2 = wire.filter((f) => f.method === "record.save").at(-1);
  assert.deepEqual(req2.params, { draftDir: "/e", saveAs: "action", new: "newcrm" }, "a new automation is --new");
  g.MonoAsk.handleFrame({ kind: "reply", id: req2.id, ok: true, data: {} });
  await creating;
});

test("analyze refuses while the recording has not reached the bridge", async () => {
  const chrome = fakeChrome();
  const g = loadExtensionScripts(["ask.js", "recorder_privacy.js", "recorder_outbox.js", "recorder_session.js", "recorder_wiring.js"], { chrome });
  g.MonoAsk.install({ send: () => false, isConnected: () => false });
  g.MonoRecorderWiring.install({ send: () => false, isConnected: () => false, storage: chrome.storage.local });
  const call = (msg) => new Promise((resolve) => chrome.runtime.onMessage.fire(msg, PANEL, resolve));
  chrome.runtime.onConnect.fire({ name: `${g.MonoRecorderWiring.PORT_NAME}:${PANEL_ID}`, sender: PANEL, onMessage: event(), onDisconnect: event(), postMessage() {} });
  await call({ type: "record_start", tabId: 7, panel: PANEL_ID });
  await call({ type: "record_stop" });
  const res = await call({ type: "record_analyze" });
  assert.equal(res.ok, false);
  assert.match(res.error, /waiting to reach the bridge/);
});

test("navigation and tab listeners exist only while recording", async () => {
  const { chrome, call, settle } = load();
  const count = () => chrome.webNavigation.onCommitted.fns.length + chrome.tabs.onCreated.fns.length;
  await settle();
  assert.equal(count(), 0, "idle: no listener on anyone's browsing");
  await call({ type: "record_start", tabId: 7 });
  assert.equal(count(), 2);
  assert.equal(chrome.webNavigation.onReferenceFragmentUpdated.fns.length, 1, "hash routes are heard");
  await call({ type: "record_stop" });
  assert.equal(count(), 0);
});

test("a hash-route change is recorded as navigation", async () => {
  const { chrome, wire, call, settle } = load();
  await call({ type: "record_start", tabId: 7 });
  chrome.webNavigation.onReferenceFragmentUpdated.fire({ tabId: 7, frameId: 0, url: "https://app.test/7#/inbox", transitionType: "link", transitionQualifiers: [] });
  await settle();
  const nav = wire.find((f) => f.event && f.event.url.endsWith("#/inbox"));
  assert.equal(nav.event.type, "navigated");
});

test("a page from the back/forward cache is told whether to keep recording", async () => {
  const { call } = load();
  await call({ type: "record_start", tabId: 7 });
  assert.deepEqual(await call({ type: "recorder_alive" }, PAGE(7)), { recording: true });
  assert.deepEqual(await call({ type: "recorder_alive" }, PAGE(8)), { recording: false });
  await call({ type: "record_stop" });
  assert.deepEqual(await call({ type: "recorder_alive" }, PAGE(7)), { recording: false });
});

test("the recording state lives in storage.session", async () => {
  const { chrome, call, settle } = load();
  await call({ type: "record_start", tabId: 7 });
  await settle();
  assert.ok(chrome.storage.session.data.recordingState);
  assert.equal(chrome.data.recordingState, undefined);
});

test("a browser restart (runtime.onStartup) closes the old recording", async () => {
  const { chrome, call, settle } = load();
  await call({ type: "record_start", tabId: 7 });
  chrome.runtime.onStartup.fire();
  await settle();
  const res = await call({ type: "record_status" });
  assert.equal(res.state.recording, false);
});

test("only this extension's pages may drive a recording, and only tabs send events", async () => {
  const { wire, call } = load();
  // An ignored message is not answered at all (call resolves undefined).
  const nobody = (msg, sender) => call(msg, sender).then((r) => (r === undefined ? "ignored" : r));
  assert.equal(await nobody({ type: "record_start", tabId: 7 }, { id: "other-extension", url: "chrome-extension://other/x.html" }), "ignored");
  assert.equal(await nobody({ type: "record_start", tabId: 7 }, PAGE(7)), "ignored", "a content script cannot start one");
  assert.equal(wire.length, 0);
  await call({ type: "record_start", tabId: 7 });
  await call({ type: "recorder_event", event: { type: "click", url: "https://app.test/7", at: 1 } }, { id: "ext", url: "chrome-extension://ext/sidepanel.html" });
  await call({ type: "recorder_event", event: { type: "click", url: "https://app.test/7", at: 1 } }, { id: "evil", tab: { id: 7 } });
  assert.equal(wire.filter((f) => f.op === "event").length, 0, "events come only from this extension's page recorders");
});

test("a failed replay reaches the panel as its report (R6-1)", async () => {
  const { g, wire, call, settle } = load();
  const verifying = call({ type: "record_verify", draftDir: "/d" });
  await settle();
  const req = wire.find((f) => f.method === "record.verify");
  const report = { ok: false, stoppedAt: null, steps: [{ id: "s1", type: "click", status: "fail", message: "no such element", selector: "#s" }] };
  g.MonoAsk.handleFrame({ kind: "reply", id: req.id, ok: true, data: report });
  const res = await verifying;
  assert.deepEqual([res.ok, res.result], [true, report]);
});

test("a verify that fails with no report is a plain error", async () => {
  const { g, wire, call, settle } = load();
  const verifying = call({ type: "record_verify", draftDir: "/d" });
  await settle();
  const req = wire.find((f) => f.method === "record.verify");
  g.MonoAsk.handleFrame({ kind: "reply", id: req.id, ok: false, error: "browser bridge not connected" });
  const res = await verifying;
  assert.deepEqual([res.ok, res.error], [false, "browser bridge not connected"]);
});

test("record_save passes keepPackageSelectors only as a real true (L2)", async () => {
  const { g, wire, call, settle } = load();
  for (const [keep, want] of [[true, true], [false, undefined], ["true", undefined]]) {
    const saving = call({ type: "record_save", draftDir: "/d", automation: "contact", keepPackageSelectors: keep });
    await settle();
    const req = wire.filter((f) => f.method === "record.save").at(-1);
    assert.equal(req.params.keepPackageSelectors, want, JSON.stringify(keep));
    g.MonoAsk.handleFrame({ kind: "reply", id: req.id, ok: true, data: {} });
    await saving;
  }
});

test("a selector_conflict refusal reaches the panel with its code and keys", async () => {
  const { g, wire, call, settle } = load();
  const saving = call({ type: "record_save", draftDir: "/d", automation: "contact" });
  await settle();
  const req = wire.filter((f) => f.method === "record.save").at(-1);
  g.MonoAsk.handleFrame({ kind: "reply", id: req.id, ok: false, error: "selectors differ", code: "selector_conflict", data: { keys: ["contact.name_input"] } });
  assert.deepEqual(await saving, { ok: false, error: "selectors differ", code: "selector_conflict", data: { keys: ["contact.name_input"] } });
});

// #451: a recording is owned by the panel that starts it and stops when that
// panel closes. A start that reaches the worker after its panel closed (a
// click, then the panel shut before the message landed) starts nothing.
test("a start from a panel that has closed starts nothing", async () => {
  const { chrome, wire, call, port, settle } = load({ panel: false });
  const p = port();
  p.onDisconnect.fire();
  await settle();
  const res = await call({ type: "record_start", tabId: 7, goal: "demo" });
  assert.equal(res.ok, false);
  assert.match(res.error, /side panel/);
  assert.deepEqual([wire.filter((f) => f.op === "start").length, chrome.scripts.length], [0, 0], "nothing injected, nothing sent");
  assert.equal((await call({ type: "record_status" })).state.recording, false);
  port();
  assert.equal((await call({ type: "record_start", tabId: 7 })).ok, true, "an open panel starts one");
});

// #876 review: every side panel has the same address and, to the worker, no
// document id (Chromium sends neither for a side panel), so a start names the
// id its panel made; it is refused unless that panel's port is open.
test("a start is the asking panel's own: no panel id, or another panel's, starts nothing", async () => {
  const { chrome, wire, call } = load();
  const none = await call({ type: "record_start", tabId: 7, panel: "" });
  const other = await call({ type: "record_start", tabId: 7, panel: "9e8f0a1b-2c3d-4e5f-8a7b-6c5d4e3f2a1b" });
  const bare = await call({ type: "record_start", tabId: 7, panel: "not-a-panel-id" });
  assert.deepEqual([none.ok, other.ok, bare.ok], [false, false, false]);
  assert.deepEqual([wire.filter((f) => f.op === "start").length, chrome.scripts.length], [0, 0]);
  assert.equal((await call({ type: "record_start", tabId: 7 })).ok, true, "the panel whose port is open starts one");
});

// #876 review: a panel owns its recording from the start. One that closes
// while its start is still under way leaves no recording behind, even while
// another window's panel is open.
const OTHER_PANEL = "9e8f0a1b-2c3d-4e5f-8a7b-6c5d4e3f2a1b";

test("a panel that closes while its recording starts stops it, whatever other panel is open", async () => {
  const { chrome, wire, call, port, settle } = load({ panel: false });
  const a = port();
  port(OTHER_PANEL);
  const get = chrome.tabs.get;
  let release;
  chrome.tabs.get = (id) => new Promise((resolve) => (release = () => resolve(get(id))));
  const started = call({ type: "record_start", tabId: 7 });
  await new Promise((r) => setTimeout(r, 20));
  a.onDisconnect.fire(); // closed mid-start
  release();
  const res = await started;
  await settle();
  assert.equal(res.ok, false);
  assert.match(res.error, /closed/);
  assert.equal((await call({ type: "record_status" })).state.recording, false);
  assert.ok(wire.some((f) => f.op === "stop" && f.reason === "panel_closed"), "what was started is stopped");
});

test("a start refused while another panel's recording runs leaves that panel the owner", async () => {
  const { wire, call, port, settle } = load({ panel: false });
  const a = port();
  port(OTHER_PANEL);
  assert.equal((await call({ type: "record_start", tabId: 7 })).ok, true);
  assert.equal((await call({ type: "record_start", tabId: 7, panel: OTHER_PANEL })).ok, false, "one recording at a time");
  a.onDisconnect.fire();
  await settle();
  assert.deepEqual([wire.at(-1).op, wire.at(-1).reason], ["stop", "panel_closed"], "closing the panel that started it still stops it");
});

// #876 review: an older panel's port carries no id ("monoagent-record"); a
// start that names no panel must not match it.
test("a start naming no panel is refused while an older panel's bare port is open", async () => {
  const { chrome, wire, call, g } = load({ panel: false });
  chrome.runtime.onConnect.fire({ name: g.MonoRecorderWiring.PORT_NAME, sender: PANEL, onMessage: event(), onDisconnect: event(), postMessage() {} });
  const res = await call({ type: "record_start", tabId: 7, panel: "" });
  assert.equal(res.ok, false);
  assert.equal(wire.filter((f) => f.op === "start").length, 0);
});
