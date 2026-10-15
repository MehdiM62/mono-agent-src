// Tests for submit_guard's worker half (agent_submit_guard.js) against a
// fake chrome; the page half runs in agent_submit.browser.test.mjs.
// `node --test 'chrome-extension/**/*.test.mjs'`.

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

/**
 * A browser whose tabs have frames. executeScript records what was injected
 * where, and plays the guard's part: installGuard arms a frame, the control
 * function acts on the frame's guard if there is one.
 */
function fakeChrome({ frames = { 7: [0, 3] }, session = {} } = {}) {
  const store = { ...session };
  const listeners = { committed: [], message: [], removed: [] };
  const guards = new Map(); // "tab:frame" -> {armed, allow}
  const calls = [];
  let page = null;
  const chrome = {
    runtime: { id: "ext-id", onMessage: { addListener: (fn) => listeners.message.push(fn) } },
    storage: {
      session: {
        get: async (key) => (key in store ? { [key]: JSON.parse(JSON.stringify(store[key])) } : {}),
        set: async (obj) => Object.assign(store, JSON.parse(JSON.stringify(obj))),
      },
    },
    webNavigation: { onCommitted: { addListener: (fn) => listeners.committed.push(fn) } },
    tabs: { onRemoved: { addListener: (fn) => listeners.removed.push(fn) } },
    scripting: {
      executeScript: async ({ target, func, args, world, injectImmediately }) => {
        const all = frames[target.tabId];
        if (!all) throw new Error(`No tab with id: ${target.tabId}`);
        const ids = target.allFrames ? all : target.frameIds;
        const kind = func === page.installRelay ? "relay" : func === page.installGuard ? "guard" : `control:${args[1]}`;
        calls.push({ kind, tabId: target.tabId, frames: ids, world: world || "ISOLATED", immediately: !!injectImmediately });
        return ids.map((frameId) => {
          const key = `${target.tabId}:${frameId}`;
          if (kind === "relay") return { frameId, result: true };
          if (kind === "guard") {
            guards.set(key, { armed: true, allow: 0 });
            return { frameId, result: true };
          }
          const g = guards.get(key);
          if (!g) return { frameId, result: false };
          if (args[1] === "disarm") g.armed = false;
          if (args[1] === "allow_once") g.allow = 1;
          return { frameId, result: true };
        });
      },
    },
  };
  return {
    chrome,
    store,
    guards,
    calls,
    setPage: (p) => (page = p),
    commit: async (tabId, frameId) => {
      listeners.committed.forEach((fn) => fn({ tabId, frameId, url: "https://shop.test/next" }));
      await new Promise((r) => setTimeout(r, 10));
    },
    report: (tabId, frameId, detail) => listeners.message.forEach((fn) => fn({ type: "agent_submit_blocked", detail }, { id: "ext-id", tab: { id: tabId }, frameId, url: `https://shop.test/frame${frameId}` })),
    remove: (tabId) => listeners.removed.forEach((fn) => fn(tabId)),
  };
}

function setup(opts) {
  const f = fakeChrome(opts);
  const g = loadExtensionScripts(["agent_cdp.js", "agent_submit_page.js", "agent_submit_guard.js"], { chrome: f.chrome });
  f.setPage(g.MonoAgentSubmitPage);
  g.MonoAgentSubmitGuard.install();
  return Object.assign(f, { guard: g.MonoAgentSubmitGuard });
}

const settle = () => new Promise((r) => setTimeout(r, 10));

const report = (fields) =>
  JSON.stringify({
    url: "https://shop.test/cart",
    form: { action: "https://shop.test/pay", method: "POST", fields, submitter: "#pay", selector: "#checkout" },
  });

test("arm puts the relay, then the guard, into every frame", async () => {
  const t = setup();
  assert.deepEqual(await t.guard.handle({ tabId: 7, action: "arm" }), { armed: true, blocked: [] });
  assert.deepEqual(t.calls.map((c) => [c.kind, c.world, c.frames]), [
    ["relay", "ISOLATED", [0, 3]],
    ["guard", "MAIN", [0, 3]],
  ]);
  assert.equal(t.store.agentSubmitGuard[7].armed, true, "kept in session storage, for a worker restart");
});

test("a blocked submit is recorded with the reporting frame's URL; status reads it back", async () => {
  const t = setup();
  await t.guard.handle({ tabId: 7, action: "arm" });
  t.report(7, 3, report([{ name: "card", type: "text", label: "Card", value: "•••" }]));
  await settle();
  const st = await t.guard.handle({ tabId: 7, action: "status" });
  assert.equal(st.armed, true);
  assert.equal(st.blocked.length, 1);
  const [b] = st.blocked;
  assert.deepEqual(Object.keys(b), ["seq", "ts", "url", "form"], "the contract's fields, no frame id");
  assert.equal(b.seq, 1);
  assert.equal(b.url, "https://shop.test/frame3", "the browser's word for the frame, not the page's");
  assert.deepEqual(b.form, { action: "https://shop.test/pay", method: "POST", fields: [{ name: "card", type: "text", label: "Card", value: "•••" }], submitter: "#pay", selector: "#checkout" });
});

test("allow_once goes to the frame the last blocked submit came from", async () => {
  const t = setup();
  await t.guard.handle({ tabId: 7, action: "arm" });
  t.report(7, 3, report([]));
  await settle();
  await t.guard.handle({ tabId: 7, action: "allow_once" });
  const last = t.calls[t.calls.length - 1];
  assert.deepEqual([last.kind, last.world, last.frames], ["control:allow_once", "MAIN", [3]]);
  assert.equal(t.guards.get("7:3").allow, 1);
  assert.equal(t.guards.get("7:0").allow, 0, "exactly one submit, not one per frame");
});

test("allow_once falls back to the top frame, and refuses when nothing is armed", async () => {
  const t = setup();
  await assert.rejects(t.guard.handle({ tabId: 7, action: "allow_once" }), /the guard is not armed on this tab/);
  await t.guard.handle({ tabId: 7, action: "arm" });
  t.report(7, 3, report([]));
  await settle();
  t.guards.delete("7:3"); // that frame navigated away
  await t.guard.handle({ tabId: 7, action: "allow_once" });
  assert.equal(t.guards.get("7:0").allow, 1);
  t.guards.clear();
  await assert.rejects(t.guard.handle({ tabId: 7, action: "allow_once" }), /no guard in this page/);
});

test("a navigation in an armed tab re-arms the new document, as early as Chrome allows", async () => {
  const t = setup();
  await t.guard.handle({ tabId: 7, action: "arm" });
  t.calls.length = 0;
  await t.commit(7, 0);
  assert.deepEqual(t.calls.map((c) => [c.kind, c.frames, c.immediately]), [
    ["relay", [0], true],
    ["guard", [0], true],
  ]);
  await t.guard.handle({ tabId: 7, action: "disarm" });
  t.calls.length = 0;
  await t.commit(7, 0);
  assert.deepEqual(t.calls, [], "disarmed: navigations are left alone");
});

test("disarm stops every frame's guard and keeps what was blocked", async () => {
  const t = setup();
  await t.guard.handle({ tabId: 7, action: "arm" });
  t.report(7, 0, report([]));
  await settle();
  const st = await t.guard.handle({ tabId: 7, action: "disarm" });
  assert.equal(st.armed, false);
  assert.equal(st.blocked.length, 1);
  assert.equal(t.guards.get("7:0").armed, false);
  assert.equal(t.guards.get("7:3").armed, false);
  t.report(7, 0, report([]));
  await settle();
  assert.equal((await t.guard.handle({ tabId: 7, action: "status" })).blocked.length, 1, "a disarmed tab records nothing");
});

test("the report is page-written data: shapes and sizes are re-checked", () => {
  const t = setup();
  assert.equal(t.guard.normalize("not json"), null);
  assert.equal(t.guard.normalize("42"), null);
  const big = t.guard.normalize(JSON.stringify({
    url: "x".repeat(5000),
    form: { method: { evil: true }, fields: Array.from({ length: 300 }, () => ({ name: 1, value: "v".repeat(900), extra: "dropped" })) },
  }));
  assert.equal(big.url.length, 2048);
  assert.equal(big.form.method, "");
  assert.equal(big.form.fields.length, 100);
  assert.deepEqual(big.form.fields[0], { name: "", type: "", label: "", value: "v".repeat(500) });
});

test("a worker restart picks the armed tabs back up from session storage", async () => {
  const saved = { agentSubmitGuard: { 7: { armed: true, seq: 4, blocked: [{ seq: 4, ts: 1, url: "u", form: {}, frameId: 0 }] } } };
  const t = setup({ session: saved });
  await t.commit(7, 0);
  assert.deepEqual(t.calls.map((c) => c.kind), ["relay", "guard"]);
  t.report(7, 0, report([]));
  await settle();
  const st = await t.guard.handle({ tabId: 7, action: "status" });
  assert.deepEqual(st.blocked.map((b) => b.seq), [4, 5], "seq keeps growing across the restart");
});

test("a tab keeps its last 50 attempts, and closing it forgets them", async () => {
  const t = setup();
  await t.guard.handle({ tabId: 7, action: "arm" });
  for (let i = 0; i < 55; i++) t.report(7, 0, report([]));
  await settle();
  const st = await t.guard.handle({ tabId: 7, action: "status" });
  assert.equal(st.blocked.length, 50);
  assert.equal(st.blocked[0].seq, 6);
  t.remove(7);
  await settle();
  assert.deepEqual(await t.guard.handle({ tabId: 7, action: "status" }), { armed: false, blocked: [] });
});

test("messages from elsewhere, and unknown actions, are refused", async () => {
  const t = setup();
  await t.guard.handle({ tabId: 7, action: "arm" });
  t.report(7, 0, report([]));
  await settle();
  await assert.rejects(t.guard.handle({ tabId: 7, action: "submit" }), /action must be one of arm, disarm, allow_once, status/);
  await assert.rejects(t.guard.handle({ action: "status" }), /tabId is required/);
  await assert.rejects(t.guard.handle({ tabId: 99, action: "arm" }), /No tab with id: 99/);
  assert.equal((await t.guard.handle({ tabId: 99, action: "status" })).armed, false, "a tab the guard never installed in stays unarmed");
});
