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
