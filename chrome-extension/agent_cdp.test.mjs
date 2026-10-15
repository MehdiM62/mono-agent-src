// Tests for the coder commands' share of the debugger (agent_cdp.js) and
// their dispatch (agent_commands.js).
// `node --test 'chrome-extension/**/*.test.mjs'`.

import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { loadExtensionScripts } from "./test_helpers.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));

function setup() {
  const g = loadExtensionScripts(["agent_cdp.js"], {});
  const calls = [];
  const listeners = { event: [], detach: [], removed: [] };
  g.MonoAgentCdp.install({
    attach: async (tabId) => calls.push(["attach", tabId]),
    cdp: async (tabId, method, params) => {
      calls.push(["cdp", tabId, method, params]);
      return { ok: method };
    },
    onDebuggerEvent: (fn) => listeners.event.push(fn),
    onDebuggerDetach: (fn) => listeners.detach.push(fn),
    onTabRemoved: (fn) => listeners.removed.push(fn),
  });
  return { cdp: g.MonoAgentCdp, calls, listeners };
}

test("send attaches through background.js's helper before every command", async () => {
  const { cdp, calls } = setup();
  assert.deepEqual(await cdp.send(4, "Page.getLayoutMetrics"), { ok: "Page.getLayoutMetrics" });
  assert.deepEqual(calls, [["attach", 4], ["cdp", 4, "Page.getLayoutMetrics", {}]]);
});

test("holds are named, so watch and emulate let go independently", () => {
  const { cdp } = setup();
  assert.equal(cdp.holds(5), false);
  cdp.hold(5, "watch");
  cdp.hold(5, "emulate");
  cdp.release(5, "watch");
  assert.equal(cdp.holds(5), true, "emulate still holds it");
  cdp.release(5, "emulate");
  assert.equal(cdp.holds(5), false);
  cdp.release(5, "emulate"); // releasing twice is harmless
});

test("Chrome detaching the debugger ends every hold and tells the modules why", () => {
  const { cdp, listeners } = setup();
  const lost = [];
  cdp.onLost((tabId, reason) => lost.push([tabId, reason]));
  cdp.hold(6, "watch");
  listeners.detach[0]({ tabId: 6 }, "canceled_by_user");
  assert.equal(cdp.holds(6), false);
  cdp.hold(7, "emulate");
  listeners.removed[0](7);
  assert.equal(cdp.holds(7), false);
  assert.deepEqual(lost, [[6, "canceled_by_user"], [7, "target_closed"]]);
});

test("events reach the modules for the page session only, and one failing listener stops none", () => {
  const { cdp, listeners } = setup();
  const seen = [];
  cdp.onEvent(() => {
    throw new Error("a broken module");
  });
  cdp.onEvent((tabId, method) => seen.push([tabId, method]));
  listeners.event[0]({ tabId: 3 }, "Runtime.consoleAPICalled", {});
  listeners.event[0]({ tabId: 3, sessionId: "child" }, "Runtime.consoleAPICalled", {});
  assert.deepEqual(seen, [[3, "Runtime.consoleAPICalled"]]);
});

test("tabOf refuses a command without a tab", () => {
  const { cdp } = setup();
  assert.equal(cdp.tabOf({ tabId: 9 }, "screenshot"), 9);
  assert.throws(() => cdp.tabOf({}, "screenshot"), /screenshot: tabId is required/);
  assert.throws(() => cdp.tabOf({ tabId: "9" }, "screenshot"), /tabId is required/);
});

test("the dispatch answers exactly the ten C1 types", async () => {
  const g = loadExtensionScripts(["agent_cdp.js", "agent_commands.js"], {});
  assert.deepEqual(g.MonoAgentCommands.TYPES, [
    "list_tabs", "tab_group", "screenshot", "emulate", "watch_tab",
    "read_console", "read_network", "a11y_tree", "submit_guard", "submit_prompt",
  ]);
  await assert.rejects(g.MonoAgentCommands.handle("cookies", {}), /Unknown command type: cookies/);
});

// ── background.js's wiring, read from the source ─────────────────────────

const background = readFileSync(join(HERE, "background.js"), "utf8");

test("background.js dispatches every C1 type to the agent modules", () => {
  const g = loadExtensionScripts(["agent_cdp.js", "agent_commands.js"], {});
  const dispatch = background.slice(background.indexOf("// The coder's browser commands (agent_commands.js)."));
  const block = dispatch.slice(0, dispatch.indexOf("break;"));
  for (const type of g.MonoAgentCommands.TYPES) assert.ok(block.includes(`case "${type}":`), `${type} is not dispatched`);
  assert.ok(block.includes("MonoAgentCommands.handle(cmd.type, params)"));
});

test("the commands that read or change the page take the origin check", () => {
  const from = background.indexOf("const SENSITIVE_COMMANDS");
  const sensitive = background.slice(from, background.indexOf("]);", from));
  for (const type of ["screenshot", "emulate", "watch_tab", "read_console", "read_network", "a11y_tree", "submit_guard", "submit_prompt"]) {
    assert.ok(sensitive.includes(`"${type}"`), `${type} is not in SENSITIVE_COMMANDS`);
  }
});

test("neither the idle sweep nor detachDebugger takes a held tab", () => {
  const sweep = background.slice(background.indexOf("function sweepIdleDebuggers()"));
  assert.match(sweep.slice(0, sweep.indexOf("\n}\n")), /if \(MonoAgentCdp\.holds\(tabId\)\) continue;/);
  const detach = background.slice(background.indexOf("async function detachDebugger(tabId) {"));
  const body = detach.slice(0, detach.indexOf("\n}\n"));
  assert.ok(body.indexOf("if (MonoAgentCdp.holds(tabId)) return;") < body.indexOf("forgetDebugger(tabId);"));
});

test("agent_cdp.js loads before the modules that register with it, and the dispatch last", () => {
  const imports = [...background.matchAll(/importScripts\(([^)]*)\)/g)].flatMap((m) => [...m[1].matchAll(/"([^"]+)"/g)].map((x) => x[1]));
  const agent = imports.filter((f) => f.startsWith("agent_"));
  assert.equal(agent[0], "agent_cdp.js");
  assert.equal(agent[agent.length - 1], "agent_commands.js");
  assert.ok(agent.indexOf("agent_submit_page.js") < agent.indexOf("agent_submit_guard.js"));
});

// ── expectOrigin and the idle release (security review, items 6 and 7) ──

/** commands loads the dispatch with recording stand-ins for the modules it calls. */
function commands({ tabs = {}, session = {} } = {}) {
  const store = { ...session };
  const messages = [];
  const chrome = {
    runtime: { id: "ext-id", onMessage: { addListener: (fn) => messages.push(fn) } },
    storage: {
      session: {
        get: async (key) => (key in store ? { [key]: JSON.parse(JSON.stringify(store[key])) } : {}),
        set: async (obj) => Object.assign(store, JSON.parse(JSON.stringify(obj))),
      },
    },
    tabs: {
      get: async (id) => {
        if (!tabs[id]) throw new Error(`No tab with id: ${id}.`);
        return tabs[id];
      },
    },
  };
  const g = loadExtensionScripts(["agent_cdp.js", "agent_commands.js"], { chrome });
  const did = [];
  let armed = [];
  const emulating = new Set();
  g.MonoAgentWatch = { watch: async (p) => (did.push(["watch", p.tabId, p.on]), g.MonoAgentCdp.release(p.tabId, "watch"), { watching: false }) };
  g.MonoAgentShot = {
    emulating: (tabId) => emulating.has(tabId),
    emulate: async (p) => (did.push(["emulate", p.tabId, p.reset]), g.MonoAgentCdp.release(p.tabId, "emulate"), { ok: true }),
  };
  g.MonoAgentSubmitGuard = {
    install: () => {},
    armedTabs: async () => armed,
    handle: async (p) => (did.push(["guard", p.tabId, p.action]), (armed = armed.filter((t) => t !== p.tabId)), {}),
  };
  let removed = null;
  g.MonoAgentCommands.install({
    attach: async () => {},
    cdp: async () => ({}),
    onDebuggerEvent: () => {},
    onDebuggerDetach: () => {},
    onTabRemoved: (fn) => (removed = fn),
    detach: async (tabId) => did.push(["detach", tabId]),
  });
  return {
    c: g.MonoAgentCommands,
    cdp: g.MonoAgentCdp,
    did,
    store,
    arm: (tabId) => armed.push(tabId),
    emulate: (tabId) => emulating.add(tabId),
    message: (msg, sender) => messages.forEach((fn) => fn(msg, sender)),
    removeTab: (tabId) => removed(tabId),
  };
}

test("expectOrigin: absent, nothing is checked; given, another origin or a navigation in flight is origin_changed", async () => {
  const t = commands({
    tabs: {
      1: { id: 1, url: "http://localhost:5173/app" },
      2: { id: 2, url: "http://localhost:5173/app", pendingUrl: "https://evil.test/" },
      3: { id: 3, url: "chrome://newtab/" },
      4: { id: 4, url: "about:blank" },
      5: { id: 5, url: "" },
      6: { id: 6, url: "https://shop.test:443/cart" },
    },
  });
  await t.c.checkOrigin({ tabId: 1 });
  await t.c.checkOrigin({ tabId: 99 }); // no param: not even the tab is looked up
  await t.c.checkOrigin({ tabId: 1, expectOrigin: "http://localhost:5173" });
  await t.c.checkOrigin({ expectOrigin: "http://localhost:5173" }); // not a tab's command
  await assert.rejects(t.c.checkOrigin({ tabId: 1, expectOrigin: "https://shop.test" }), /^Error: origin_changed: the tab is on http:\/\/localhost:5173, not https:\/\/shop.test$/);
  await assert.rejects(t.c.checkOrigin({ tabId: 2, expectOrigin: "http://localhost:5173" }), /^Error: origin_changed: the tab is navigating to https:\/\/evil.test/);
  await assert.rejects(t.c.checkOrigin({ tabId: 3, expectOrigin: "http://localhost:5173" }), /^Error: origin_changed: the tab is on no web page, not http:\/\/localhost:5173$/);
  await assert.rejects(t.c.checkOrigin({ tabId: 4, expectOrigin: "null" }), /origin_changed: the tab is on no web page/, "about:blank matches nothing");
  await assert.rejects(t.c.checkOrigin({ tabId: 5, expectOrigin: "" + "http://localhost:5173" }), /origin_changed: the tab is on no web page/);
  await t.c.checkOrigin({ tabId: 6, expectOrigin: "https://shop.test" }); // URL.origin drops the default port
  await assert.rejects(t.c.checkOrigin({ tabId: 1, expectOrigin: 7 }), /origin_changed: expectOrigin must be an origin/);
});

test("a held or armed tab with no command for IDLE_RELEASE_MS is unwatched, reset, disarmed and detached", async () => {
  const t = commands();
  assert.equal(t.c.IDLE_RELEASE_MS, 15 * 60 * 1000);
  t.cdp.hold(4, "watch");
  t.cdp.hold(4, "emulate");
  t.emulate(4);
  t.arm(4);
  t.c.touch(4);
  await t.c.sweepIdle(Date.now() + 1000);
  assert.deepEqual(t.did, [], "not idle yet");
  await t.c.sweepIdle(Date.now() + t.c.IDLE_RELEASE_MS + 1000);
  assert.deepEqual(t.did, [["watch", 4, false], ["emulate", 4, true], ["guard", 4, "disarm"], ["detach", 4]]);
  assert.equal(t.cdp.holds(4), false);
});

test("an armed tab the guard kept across a worker restart gets the full idle time from the restart", async () => {
  const t = commands();
  t.arm(6);
  await t.c.sweepIdle(Date.now() + t.c.IDLE_RELEASE_MS - 60000);
  assert.deepEqual(t.did, []);
  await t.c.sweepIdle(Date.now() + t.c.IDLE_RELEASE_MS + 60000);
  assert.deepEqual(t.did, [["watch", 6, false], ["guard", 6, "disarm"], ["detach", 6]]);
});

test("background.js counts every tab's command as use, checks expectOrigin before the handlers, and sweeps on its alarm", () => {
  const dispatch = background.slice(background.indexOf("async function handleCommand(cmd) {"));
  const body = dispatch.slice(0, dispatch.indexOf("\n}\n"));
  const check = body.indexOf("await MonoAgentCommands.checkOrigin(params);");
  assert.ok(body.indexOf("MonoAgentCommands.touch(params.tabId);") !== -1);
  assert.ok(check !== -1 && check > body.indexOf("ACTIVE_TAB_COMMANDS.has") && check < body.indexOf("switch (cmd.type)"));
  const alarm = background.slice(background.indexOf("chrome.alarms.onAlarm.addListener"));
  assert.match(alarm.slice(0, alarm.indexOf("\n});\n")), /MonoAgentCommands\.sweepIdle\(\)/);
  const install = background.slice(background.indexOf("MonoAgentCommands.install({"));
  assert.match(install.slice(0, install.indexOf("\n});")), /detach: \(tabId\) => \(debuggerPinned\.has\(tabId\) \? undefined : detachDebugger\(tabId\)\)/);
});

test("Stop agent guard, pressed in the page, lets the tab go at once and takes the bar down", async () => {
  const t = commands();
  t.cdp.hold(4, "watch");
  t.arm(4);
  t.c.touch(4);
  t.message({ type: "agent_guard_stop" }, { id: "elsewhere", tab: { id: 4 } }); // not this extension's page
  t.message({ type: "agent_guard_stop" }, { id: "ext-id" }); // no tab
  await new Promise((r) => setTimeout(r, 10));
  assert.deepEqual(t.did, []);
  t.message({ type: "agent_guard_stop" }, { id: "ext-id", tab: { id: 4 } });
  await new Promise((r) => setTimeout(r, 10));
  assert.deepEqual(t.did, [["watch", 4, false], ["guard", 4, "disarm"], ["detach", 4], ["guard", 4, "disarm"]]);
  assert.equal(t.cdp.holds(4), false);
});

test("the idle clock survives a worker restart: the times are kept in session storage", async () => {
  const t = commands();
  t.c.touch(9);
  await new Promise((r) => setTimeout(r, 10));
  const kept = t.store.agentLastUsed[9];
  assert.ok(Number.isFinite(kept), "written at once");
  // A restarted worker on the same storage, its tab used 14 minutes before the restart.
  const before = Date.now() - 14 * 60 * 1000;
  const restarted = commands({ session: { agentLastUsed: { 9: before } } });
  restarted.arm(9);
  await restarted.c.sweepIdle(before + restarted.c.IDLE_RELEASE_MS + 1000);
  assert.deepEqual(restarted.did, [["watch", 9, false], ["guard", 9, "disarm"], ["detach", 9]], "15 minutes after its last use, not after the restart");
  assert.equal(restarted.store.agentLastUsed[9], undefined, "a released tab is forgotten");
});

test("touch writes a tab's time at most every 15 seconds, and a closed tab is forgotten", async () => {
  const t = commands();
  t.c.touch(3);
  await new Promise((r) => setTimeout(r, 5));
  const first = t.store.agentLastUsed[3];
  t.c.touch(3);
  await new Promise((r) => setTimeout(r, 5));
  assert.equal(t.store.agentLastUsed[3], first, "not written again within 15s");
  t.removeTab(3);
  t.c.touch(5);
  await new Promise((r) => setTimeout(r, 5));
  assert.equal(t.store.agentLastUsed[3], undefined);
});
