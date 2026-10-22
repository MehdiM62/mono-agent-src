/**
 * MonoAgent Bridge — submit observation for parked applications (epic #951, #956)
 *
 * The agent fills an application form and leaves its tab open; the person
 * submits it there. This module notices that, and tells the bridge
 * `form.submit_observed {tab_id, origin, url}` so the app can ask "Looks
 * submitted - confirm". It is a hint only: the bridge never moves an
 * application to applied on it (that is `application send`, the person's).
 *
 * What counts as an observation, and nothing else:
 *   1. a `submit` event with event.isTrusted (a person's click or Enter; page
 *      script cannot make one, and form.submit()/requestSubmit() from script
 *      fire no trusted event), in the TOP frame of a tab that sits in an agent
 *      tab group ("Coder · ..."), then
 *   2. within WINDOW_MS, either a same-tab navigation (webNavigation
 *      onCommitted or onHistoryStateUpdated, top frame, not a reload), or an
 *      in-page change of the submitted form (it is removed or hidden).
 * A page that merely displays "Application submitted" does neither: no
 * trusted submit, no observation. A trusted submit that leads nowhere (the
 * page rejected it and the form stays) reports nothing.
 *
 * The origin comes from Chrome's description of the sender's tab, never from
 * a message. The url sent has no query or fragment (they can hold tokens).
 *
 * Node-tested against a fake chrome (submit_observe.test.mjs); the page half
 * is covered by driving its listener with trusted and untrusted events.
 */

(function (root) {
  "use strict";

  const METHOD = "form.submit_observed";
  const MESSAGE = "monoagent_submit_observe";
  const WINDOW_MS = 10000;
  // The title prefix of every agent tab group (browserctl.GroupTitle).
  const GROUP_PREFIX = "Coder · ";
  const OBSERVER_KEY = "__monoagentSubmitObserve";

  /**
   * installObserver runs in the tab's top frame, in the extension's isolated
   * world (page script cannot see its window). Self-contained: it is
   * serialised by chrome.scripting.executeScript.
   */
  function installObserver(key, message, windowMs) {
    if (window[key]) return true;
    window[key] = true;
    window.addEventListener(
      "submit",
      (e) => {
        if (!e.isTrusted) return; // script-made submits do not count
        const form = e.target;
        if (!(form instanceof HTMLFormElement)) return;
        try {
          chrome.runtime.sendMessage({ type: message, stage: "submit" });
        } catch {
          return;
        }
        const gone = () => {
          if (!form.isConnected || form.hidden) return true;
          const cs = getComputedStyle(form);
          return cs.display === "none" || cs.visibility === "hidden";
        };
        let timer = null;
        const mo = new MutationObserver(() => {
          if (!gone()) return;
          stop();
          try {
            chrome.runtime.sendMessage({ type: message, stage: "form_changed" });
          } catch {
            // the extension went away
          }
        });
        const stop = () => {
          mo.disconnect();
          clearTimeout(timer);
        };
        mo.observe(document.documentElement, { subtree: true, childList: true, attributes: true });
        timer = setTimeout(stop, windowMs);
      },
      true
    );
    return true;
  }

  const pending = new Map(); // tabId -> {ts, origin}

  const originOf = (u) => {
    try {
      const o = new URL(u);
      return /^https?:$/.test(o.protocol) ? o.origin : "";
    } catch {
      return "";
    }
  };

  const bareUrl = (u) => {
    try {
      const x = new URL(u);
      return x.origin + x.pathname;
    } catch {
      return "";
    }
  };

  const fresh = (p, now) => !!p && now - p.ts <= WINDOW_MS;

  /** inAgentGroup: the tab is in a "Coder · ..." group, the only tabs this watches. */
  async function inAgentGroup(tabId) {
    if (!chrome.tabGroups) return false;
    try {
      const tab = await chrome.tabs.get(tabId);
      if (!Number.isInteger(tab.groupId) || tab.groupId < 0) return false;
      const g = await chrome.tabGroups.get(tab.groupId);
      return typeof g.title === "string" && g.title.startsWith(GROUP_PREFIX);
    } catch {
      return false;
    }
  }

  /** report sends the observation, once, when the bridge can take it. */
  async function report(tabId, url) {
    const p = pending.get(tabId);
    pending.delete(tabId);
    if (!p || !p.origin) return;
    const Ask = root.MonoAsk;
    if (!Ask || !(await Ask.supports(METHOD).catch(() => false))) return;
    await Ask.request(METHOD, { tab_id: tabId, origin: p.origin, url: bareUrl(url) || p.origin }).catch(() => {});
  }

  async function committed(details) {
    if (details.frameId !== 0 || !/^https?:/.test(details.url || "")) return;
    const p = pending.get(details.tabId);
    // The navigation the submit caused (a reload does not count).
    if (fresh(p, Date.now()) && details.transitionType !== "reload") await report(details.tabId, details.url);
    else pending.delete(details.tabId);
    // Every document of an agent's tab gets the observer: the next form may be on this page.
    if (await inAgentGroup(details.tabId)) {
      await chrome.scripting
        .executeScript({ target: { tabId: details.tabId, frameIds: [0] }, func: installObserver, args: [OBSERVER_KEY, MESSAGE, WINDOW_MS] })
        .catch(() => {});
    }
  }

  async function historyUpdated(details) {
    if (details.frameId !== 0) return;
    const p = pending.get(details.tabId);
    if (p && fresh(p, Date.now())) await report(details.tabId, details.url);
  }

  function onMessage(msg, sender) {
    if (!msg || msg.type !== MESSAGE) return false;
    const tabId = sender && sender.tab && sender.tab.id;
    if (!Number.isInteger(tabId) || sender.frameId !== 0) return false;
    if (sender.id && sender.id !== chrome.runtime.id) return false;
    if (msg.stage === "submit") {
      pending.set(tabId, { ts: Date.now(), origin: originOf(sender.tab.url) });
    } else if (msg.stage === "form_changed") {
      const p = pending.get(tabId);
      if (fresh(p, Date.now()) && originOf(sender.tab.url) === p.origin) report(tabId, sender.tab.url).catch(() => {});
    }
    return false;
  }

  /** install registers the listeners; called once, at worker start. */
  function install() {
    chrome.runtime.onMessage.addListener(onMessage);
    chrome.webNavigation.onCommitted.addListener((d) => {
      committed(d).catch(() => {});
    });
    if (chrome.webNavigation.onHistoryStateUpdated) {
      chrome.webNavigation.onHistoryStateUpdated.addListener((d) => {
        historyUpdated(d).catch(() => {});
      });
    }
    chrome.tabs.onRemoved.addListener((tabId) => pending.delete(tabId));
  }

  root.MonoSubmitObserve = { install, installObserver, METHOD, MESSAGE, WINDOW_MS, OBSERVER_KEY };
})(globalThis);
