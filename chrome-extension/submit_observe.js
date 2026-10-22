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
 *   1. a `submit` event in the TOP frame of a tab in an agent tab group
 *      ("Coder · ..."), that is isTrusted, that follows a real user gesture
 *      (a trusted click on the form's submit control, which is the event's
 *      submitter, or a trusted Enter inside the form) within 3 s, and that the
 *      page did not cancel (not defaultPrevented after its handlers ran). A
 *      page's own form.requestSubmit() is trusted too, which is why the
 *      gesture is required; and
 *   2. within WINDOW_MS, a same-tab NAVIGATION (webNavigation onCommitted or
 *      onHistoryStateUpdated, top frame, not a reload). The form merely being
 *      hidden or removed counts for nothing: a page can do that itself.
 * A page that merely displays "Application submitted" does neither. A residual
 * remains: a page can call requestSubmit(submitter) right after a real click
 * and then navigate; that only produces the hint, which asks the person.
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
   *
   * isTrusted alone proves nothing: a page's own form.requestSubmit() fires a
   * trusted submit event. So a submit counts only when (a) a trusted click on
   * the form's own submit control, or a trusted Enter inside the form (not in a
   * textarea), came within GESTURE_MS before it, the click's control being the
   * event's submitter, and (b) the submit was not defaultPrevented once the
   * page's handlers had run.
   */
  function installObserver(key, message) {
    if (window[key]) return true;
    window[key] = true;
    const GESTURE_MS = 3000;
    let gesture = null; // the latest trusted click or Enter: {ts, kind, target}
    const submitControl = (el) => {
      const c = el && el.closest ? el.closest("button,input") : null;
      if (!c) return null;
      const type = (c.getAttribute("type") || (c.tagName === "BUTTON" ? "submit" : "")).toLowerCase();
      return type === "submit" || type === "image" ? c : null;
    };
    window.addEventListener(
      "click",
      (e) => {
        if (e.isTrusted) gesture = { ts: Date.now(), kind: "click", target: e.target };
      },
      true
    );
    window.addEventListener(
      "keydown",
      (e) => {
        if (e.isTrusted && e.key === "Enter") gesture = { ts: Date.now(), kind: "enter", target: e.target };
      },
      true
    );
    window.addEventListener(
      "submit",
      (e) => {
        if (!e.isTrusted) return; // script-made events do not count
        const form = e.target;
        if (!(form instanceof HTMLFormElement)) return;
        const g = gesture;
        gesture = null;
        if (!g || Date.now() - g.ts > GESTURE_MS) return; // no person just did this
        if (g.kind === "click") {
          const c = submitControl(g.target);
          if (!c || c.form !== form || e.submitter !== c) return;
        } else if (!g.target || g.target.tagName === "TEXTAREA" || !form.contains(g.target)) {
          return;
        }
        // After the page's handlers have run: a submit it cancelled sent nothing.
        setTimeout(() => {
          if (e.defaultPrevented) return;
          try {
            chrome.runtime.sendMessage({ type: message, stage: "submit" });
          } catch {
            // the extension went away
          }
        }, 0);
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
        .executeScript({ target: { tabId: details.tabId, frameIds: [0] }, func: installObserver, args: [OBSERVER_KEY, MESSAGE] })
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
