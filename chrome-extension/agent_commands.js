/**
 * MonoAgent Bridge — the coder's browser commands (plan C1)
 *
 * One entry point for background.js's dispatch: the command types the
 * coder's `monoagentcli browser …` sends, each answered by its agent_*.js
 * module. The Go side's typed helpers are internal/extension/page_agent.go.
 *
 * Load order (background.js): agent_cdp.js first, as every other module
 * registers with it when it loads; this file last.
 *
 * Two things here run for every command background.js dispatches, not only
 * these ten (two lines in handleCommand):
 *
 *   - checkOrigin: a caller that sends `expectOrigin` (the CLI does, for an
 *     agent) is refused with an error starting "origin_changed:" when the
 *     tab is on another origin, or still navigating, by the time the command
 *     arrives. Without the param nothing is checked.
 *   - touch: the time of the tab's last command. A tab the coder's commands
 *     hold (a watch, an emulation) or have armed (the submit guard) that
 *     sees no command for IDLE_RELEASE_MS is let go by sweepIdle, on
 *     background.js's keep-alive alarm: unwatched, emulation reset,
 *     disarmed and detached, so a session the agent forgot cannot keep
 *     Chrome's debugging bar up or stop the person's submits for good.
 */

(function (root) {
  "use strict";

  const TYPES = [
    "list_tabs",
    "tab_group",
    "screenshot",
    "emulate",
    "watch_tab",
    "read_console",
    "read_network",
    "a11y_tree",
    "submit_guard",
    "submit_prompt",
  ];

  // How long a held or armed tab may go without a command before it is let go.
  const IDLE_RELEASE_MS = 15 * 60 * 1000;

  let deps = null;
  const lastUsed = new Map(); // tabId -> time of its last command
  // A tab with no command since this worker started (a restart forgets the
  // times) gets the full idle time from now.
  const startedAt = Date.now();

  /** install hands agent_cdp.js the debugger helpers (plus detach) and starts the submit guard's listeners. */
  function install(d) {
    deps = d;
    root.MonoAgentCdp.install(d);
    root.MonoAgentSubmitGuard.install();
  }

  function touch(tabId) {
    if (Number.isInteger(tabId)) lastUsed.set(tabId, Date.now());
  }

  function originOf(u) {
    try {
      return new URL(u).origin;
    } catch {
      return "";
    }
  }

  /** checkOrigin refuses a command whose tab is not on params.expectOrigin (when given). */
  async function checkOrigin(params) {
    const expect = params && params.expectOrigin;
    if (expect === undefined || expect === null || expect === "") return;
    if (typeof expect !== "string") throw new Error("origin_changed: expectOrigin must be an origin");
    if (!Number.isInteger(params.tabId)) return; // not a tab's command
    const tab = await chrome.tabs.get(params.tabId);
    if (tab.pendingUrl) {
      throw new Error(`origin_changed: the tab is navigating to ${originOf(tab.pendingUrl) || "another page"}, not settled on ${expect}`);
    }
    const now = originOf(tab.url);
    if (now !== expect) throw new Error(`origin_changed: the tab is on ${now || "no web page"}, not ${expect}`);
  }

  /** sweepIdle lets go of every held or armed tab that has had no command for IDLE_RELEASE_MS. */
  async function sweepIdle(now = Date.now()) {
    const tabs = new Set([...root.MonoAgentCdp.heldTabs(), ...(await root.MonoAgentSubmitGuard.armedTabs())]);
    for (const tabId of tabs) {
      if (now - (lastUsed.has(tabId) ? lastUsed.get(tabId) : startedAt) < IDLE_RELEASE_MS) continue;
      await release(tabId);
    }
  }

  async function release(tabId) {
    const step = (p) => Promise.resolve(p).catch((err) => console.warn("[monoagent] idle release:", err.message));
    await step(root.MonoAgentWatch.watch({ tabId, on: false }));
    if (root.MonoAgentShot.emulating(tabId)) await step(root.MonoAgentShot.emulate({ tabId, reset: true }));
    if ((await root.MonoAgentSubmitGuard.armedTabs()).includes(tabId)) {
      await step(root.MonoAgentSubmitGuard.handle({ tabId, action: "disarm" }));
    }
    lastUsed.delete(tabId);
    if (deps && deps.detach) await step(deps.detach(tabId));
  }

  async function handle(type, params) {
    const p = params || {};
    switch (type) {
      case "list_tabs":
        return root.MonoAgentTabs.listTabs();
      case "tab_group":
        return root.MonoAgentTabs.tabGroup(p);
      case "screenshot":
        return root.MonoAgentShot.screenshot(p);
      case "emulate":
        return root.MonoAgentShot.emulate(p);
      case "watch_tab":
        return root.MonoAgentWatch.watch(p);
      case "read_console":
        return root.MonoAgentWatch.readConsole(p);
      case "read_network":
        return root.MonoAgentWatch.readNetwork(p);
      case "a11y_tree":
        return root.MonoAgentA11y.tree(p);
      case "submit_guard":
        return root.MonoAgentSubmitGuard.handle(p);
      case "submit_prompt":
        return root.MonoAgentSubmitPrompt.show(p);
      default:
        throw new Error(`Unknown command type: ${type}`);
    }
  }

  root.MonoAgentCommands = { TYPES, IDLE_RELEASE_MS, install, handle, touch, checkOrigin, sweepIdle };
})(globalThis);
