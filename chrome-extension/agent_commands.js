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
 *     Chrome's debugging bar up or stop the person's submits for good. The
 *     times are kept in chrome.storage.session too (at most one write per
 *     tab every PERSIST_EVERY_MS), so a worker restart does not start the
 *     clock again.
 *
 * The armed tab's "Stop agent guard" button (agent_guard_bar.js) does the
 * same release at once, for the person.
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

  const STORE_KEY = "agentLastUsed";
  const PERSIST_EVERY_MS = 15000;
  const STOP_MESSAGE = "agent_guard_stop";

  let deps = null;
  const lastUsed = new Map(); // tabId -> time of its last command
  const persistedAt = new Map(); // tabId -> when its time was last written
  // A tab with no recorded command gets the full idle time from this
  // worker's start.
  const startedAt = Date.now();
  let loaded = null;

  /** loadTimes reads the times an earlier worker kept; what this one set since wins. */
  function loadTimes() {
    if (!loaded) {
      loaded = Promise.resolve()
        .then(() => chrome.storage.session.get(STORE_KEY))
        .then(
          (got) => {
            const saved = (got && got[STORE_KEY]) || {};
            for (const [k, t] of Object.entries(saved)) {
              const tabId = Number(k);
              if (Number.isInteger(tabId) && Number.isFinite(t) && !lastUsed.has(tabId)) lastUsed.set(tabId, t);
            }
          },
          () => {}
        );
    }
    return loaded;
  }

  function persist() {
    try {
      chrome.storage.session.set({ [STORE_KEY]: Object.fromEntries(lastUsed) }).catch(() => {});
    } catch {
      // no session storage: the times live as long as this worker
    }
  }

  /** install hands agent_cdp.js the debugger helpers (plus detach), starts the submit guard's listeners and the bar's stop button. */
  function install(d) {
    deps = d;
    root.MonoAgentCdp.install(d);
    root.MonoAgentSubmitGuard.install();
    chrome.runtime.onMessage.addListener((msg, sender) => {
      if (!msg || msg.type !== STOP_MESSAGE) return false;
      const tabId = sender && sender.tab && sender.tab.id;
      if (!Number.isInteger(tabId) || (sender.id && sender.id !== chrome.runtime.id)) return false;
      // Disarmed even when this worker thought it was not: the bar goes either way.
      release(tabId)
        .then(() => root.MonoAgentSubmitGuard.handle({ tabId, action: "disarm" }))
        .catch((err) => console.warn("[monoagent] stop agent guard:", err.message));
      return false;
    });
    root.MonoAgentCdp.onLost((tabId, reason) => {
      if (reason !== "target_closed") return;
      lastUsed.delete(tabId);
      persistedAt.delete(tabId);
    });
    loadTimes();
  }

  function touch(tabId) {
    if (!Number.isInteger(tabId)) return;
    const now = Date.now();
    lastUsed.set(tabId, now);
    if (now - (persistedAt.get(tabId) || 0) >= PERSIST_EVERY_MS) {
      persistedAt.set(tabId, now);
      persist();
    }
  }

  function originOf(u) {
    try {
      return new URL(u).origin;
    } catch {
      return "";
    }
  }

  /**
   * checkOrigin refuses a command whose tab is not on params.expectOrigin
   * (when given), compared with `new URL(tab.url).origin`: scheme://host and
   * a port only when it is not the scheme's default.
   */
  async function checkOrigin(params) {
    const expect = params && params.expectOrigin;
    if (expect === undefined || expect === null || expect === "") return;
    if (typeof expect !== "string") throw new Error("origin_changed: expectOrigin must be an origin");
    if (!Number.isInteger(params.tabId)) return; // not a tab's command
    const tab = await chrome.tabs.get(params.tabId);
    if (tab.pendingUrl) {
      throw new Error(`origin_changed: the tab is navigating to ${originOf(tab.pendingUrl) || "another page"}, not settled on ${expect}`);
    }
    // URL.origin of the committed page; about:blank, data: and an empty tab
    // ("null" or "") match nothing, not even an expectOrigin of "null".
    const now = originOf(tab.url);
    if (!now || now === "null") throw new Error(`origin_changed: the tab is on no web page, not ${expect}`);
    if (now !== expect) throw new Error(`origin_changed: the tab is on ${now}, not ${expect}`);
  }

  /** sweepIdle lets go of every held or armed tab that has had no command for IDLE_RELEASE_MS. */
  async function sweepIdle(now = Date.now()) {
    await loadTimes();
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
    persistedAt.delete(tabId);
    persist();
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
