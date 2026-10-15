/**
 * MonoAgent Bridge — the coder's commands' share of chrome.debugger
 *
 * The coder's browser commands (agent_*.js, plan C1) need the debugger for
 * screenshots, emulation, the accessibility tree and the console/network
 * watch. They go through background.js's own per-tab attach and send — the
 * ones capture.js and cdp_proxy.js use — so a tab is never attached twice
 * and everyone's traffic keeps the idle sweep's clock.
 *
 * Two of them must outlive that sweep: a watch listens without sending, and
 * an emulation lasts only while the debugger stays attached. Each HOLDS its
 * tab here, under its own name so they let go independently, and
 * background.js's sweep and detachDebugger leave a held tab attached.
 * Without that, capture.js (which detaches after every capture) or a relay
 * client's cdp_detach would end a watch nobody stopped.
 *
 * What ends a hold anyway is Chrome detaching the debugger: the person
 * pressed Cancel on the debugging banner, DevTools opened on the tab, or the
 * tab closed. onLost tells each module, which stops rather than attaching
 * again — the person's Cancel is an answer.
 *
 * Pure apart from what install() is handed, so it runs under node
 * (agent_cdp.test.mjs).
 */

(function (root) {
  "use strict";

  let deps = null;
  const held = new Map(); // tabId -> Set of hold names
  const eventListeners = [];
  const lostListeners = [];

  /**
   * install takes background.js's helpers: attach(tabId), cdp(tabId, method,
   * params), and the registrations onDebuggerEvent, onDebuggerDetach and
   * onTabRemoved.
   */
  function install(d) {
    deps = d;
    d.onDebuggerEvent((source, method, params) => {
      // A child session (an out-of-process iframe) is the relay's business.
      if (!source || source.sessionId || source.tabId === undefined) return;
      for (const fn of eventListeners) {
        try {
          fn(source.tabId, method, params || {});
        } catch (err) {
          console.warn("[monoagent] agent event:", err.message);
        }
      }
    });
    d.onDebuggerDetach((source, reason) => {
      if (source && source.tabId !== undefined) lost(source.tabId, reason || "detached");
    });
    if (d.onTabRemoved) d.onTabRemoved((tabId) => lost(tabId, "target_closed"));
  }

  /** send runs one CDP command on the tab, attaching first when needed. */
  async function send(tabId, method, params) {
    if (!deps) throw new Error("agent commands are not installed");
    await deps.attach(tabId);
    return deps.cdp(tabId, method, params || {});
  }

  function hold(tabId, name) {
    if (!held.has(tabId)) held.set(tabId, new Set());
    held.get(tabId).add(name);
  }

  function release(tabId, name) {
    const names = held.get(tabId);
    if (!names) return;
    names.delete(name);
    if (!names.size) held.delete(tabId);
  }

  /** holds is what background.js asks before it detaches a tab. */
  function holds(tabId) {
    return held.has(tabId);
  }

  /** heldTabs lists the tabs something holds, for the idle release (agent_commands.js). */
  function heldTabs() {
    return [...held.keys()];
  }

  /** lost ends every hold on the tab and tells the modules why. */
  function lost(tabId, reason) {
    held.delete(tabId);
    for (const fn of lostListeners) {
      try {
        fn(tabId, reason);
      } catch (err) {
        console.warn("[monoagent] agent detach:", err.message);
      }
    }
  }

  /** tabOf reads params.tabId, or throws naming the command that needed it. */
  function tabOf(params, type) {
    const tabId = params && params.tabId;
    if (!Number.isInteger(tabId) || tabId < 0) throw new Error(`${type}: tabId is required`);
    return tabId;
  }

  root.MonoAgentCdp = {
    install,
    send,
    tabOf,
    hold,
    release,
    holds,
    heldTabs,
    onEvent: (fn) => eventListeners.push(fn),
    onLost: (fn) => lostListeners.push(fn),
  };
})(globalThis);
