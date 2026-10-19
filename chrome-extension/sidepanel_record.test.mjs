// The Record section's seam for the chat's recording cards (sidepanel_record.js,
// MonoPanelRecord, issue #451): start() is the Record button's own start,
// analyze() its Analyze, and a draft is still saved only by the Save button,
// whose save the chat hears about. Against a fake document and worker.
// `CHROME_PATH=/nonexistent node --test chrome-extension/sidepanel_record.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

class Node_ {
  constructor(tag) {
    this.tag = tag; this.children = []; this.parentNode = null; this.dataset = {}; this.listeners = {}; this._text = "";
    this.hidden = false; this.disabled = false; this.value = ""; this.checked = false; this.open = false; this.className = "";
    const classes = new Set();
    this.classList = { toggle: (c, on) => (on ? classes.add(c) : classes.delete(c)), contains: (c) => classes.has(c) };
  }
  appendChild(c) { c.parentNode = this; this.children.push(c); return c; }
  remove() { if (this.parentNode) this.parentNode.children.splice(this.parentNode.children.indexOf(this), 1); this.parentNode = null; }
  all() { return this.children.flatMap((c) => [c, ...c.all()]); }
  contains(n) { return !!n && (n === this || this.all().includes(n)); }
  querySelectorAll(sel) {
    if (sel === "input[data-input]") return this.all().filter((n) => n.tag === "input" && n.dataset.input !== undefined);
    return this.all().filter((n) => sel.startsWith(".") && n.className.split(" ").includes(sel.slice(1)));
  }
  addEventListener(t, fn) { (this.listeners[t] = this.listeners[t] || []).push(fn); }
  fire(t) { return Promise.all((this.listeners[t] || []).map((fn) => fn({}))); }
  scrollIntoView() { this.scrolled = true; }
  set textContent(v) { this._text = String(v); this.children = []; }
  get textContent() { return this._text + this.children.map((c) => c.textContent).join(""); }
}

const REPLIES = {
  record_start: (m) => ({ ok: true, state: { recording: true, id: "rec-1", goal: m.goal, steps: [] } }),
  record_analyze: () => ({ ok: true, result: { draftDir: "/drafts/d1", draft: { names: { action: "export", automation: "reports" } } } }),
  record_save: () => ({ ok: true, result: { automation: "reports", action: "export" } }),
};

function setup(replies = {}) {
  const els = {};
  const doc = {
    getElementById: (id) => (els[id] = els[id] || new Node_(id)),
    createElement: (t) => new Node_(t),
    createTextNode: (t) => Object.assign(new Node_("#text"), { _text: t }),
  };
  const sent = [];
  const ask = async (m) => {
    sent.push(m);
    const r = Object.assign({}, REPLIES, replies)[m.type];
    return typeof r === "function" ? r(m) : r || { ok: true };
  };
  const port = { posted: [], postMessage: (m) => port.posted.push(m), onMessage: { addListener: (fn) => (port.push = fn) }, onDisconnect: { addListener() {} } };
  const chrome = { runtime: { connect: () => port } };
  const env = { document: doc, ask, chrome, setInterval: () => 0, MonoPanelPage: { current: () => ({ tabId: 5 }) } };
  const sandbox = loadExtensionScripts(["sidepanel_record_view.js", "sidepanel_record.js"], env);
  const state = (s) => port.push({ type: "record_state", state: Object.assign({ recording: false, steps: [] }, s) });
  return { el: doc.getElementById, sent, port, state, R: sandbox.MonoPanelRecord, of: (type) => sent.filter((m) => m.type === type) };
}

test("a chat card starts a recording the way the Record button does, and never over a running one", async () => {
  const t = setup();
  assert.equal(t.of("record_start").length, 0, "loading the panel starts nothing");
  const st = await t.R.start("Export the report");
  assert.deepEqual(t.of("record_start"), [{ type: "record_start", goal: "Export the report", tabId: 5 }]);
  assert.equal(st.id, "rec-1");
  assert.ok(t.port.posted.some((m) => m.type === "record_owner"), "this panel owns it: closing the panel stops it");
  assert.equal(t.el("rec-goal").value, "Export the report", "the goal is in the box, as if typed");
  assert.ok(t.el("record-panel").open);

  t.state({ recording: true, id: "rec-1" });
  await assert.rejects(t.R.start("Something else"), /already running/);
  assert.equal(t.of("record_start").length, 1);
});

test("the Record button still starts with the typed goal and stops a running recording", async () => {
  const t = setup();
  t.el("rec-goal").value = "  Typed goal ";
  await t.el("rec-toggle").fire("click");
  assert.deepEqual(t.of("record_start"), [{ type: "record_start", goal: "Typed goal", tabId: 5 }]);
  t.state({ recording: true, id: "rec-1" });
  await t.el("rec-toggle").fire("click");
  assert.equal(t.of("record_stop").length, 1);
});

test("analyze draws that recording's draft; only Save saves it, and the save is reported", async () => {
  const t = setup();
  const events = [];
  t.R.onChange((e) => events.push(e));
  t.state({ id: "rec-1", delivered: true, steps: [{ id: "e1", type: "click" }] });
  assert.equal(events.at(-1).type, "state");
  assert.equal(events.at(-1).state.id, "rec-1");

  await t.R.analyze("rec-1");
  assert.deepEqual(t.of("record_analyze"), [{ type: "record_analyze", recordingId: "rec-1" }]);
  assert.equal(t.el("rec-draft").hidden, false);
  assert.equal(t.of("record_save").length, 0, "analysis saves nothing");
  assert.ok(!events.some((e) => e.type === "saved"));

  await t.el("rec-save").fire("click");
  assert.equal(t.of("record_save").length, 1);
  assert.deepEqual(events.at(-1), { type: "saved", recordingId: "rec-1", text: "Saved as reports.export." });
});

test("a failed analysis is said in the Record section and rejects for the card", async () => {
  const t = setup({ record_analyze: { ok: false, error: "the bridge is not running" } });
  t.state({ id: "rec-1", delivered: true, steps: [{ id: "e1", type: "click" }] });
  await assert.rejects(t.R.analyze("rec-1"), /the bridge is not running/);
  assert.match(t.el("rec-msg").textContent, /Analysis failed: the bridge is not running/);
});
