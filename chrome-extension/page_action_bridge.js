/**
 * MonoAgent Bridge — doing what a chat card proposed on the page (the worker's half)
 *
 *   page_action_run {tab: {tabId, url}, proposal: {action, target?, text?, url?}}
 *     -> {ok:true, method: "script"|"debugger"|"tab"}
 *      | {ok:false, code, error, method?}
 *        codes: invalid, page_changed, unknown_outcome, not_found, ambiguous, submit_blocked,
 *               not_allowed, unavailable
 *
 * The side panel's chat may propose a click, double-click, typing or a new
 * tab (propose_page_action, chat_proposals_act.js). Only the person's click on
 * the card's Run sends this, and only from the panel. Here, at that moment:
 *
 *   1. the proposal is checked again (the card's own rules);
 *   2. open_url opens the address in a NEW tab (http/https only) and is done;
 *   3. otherwise the tab must still be on the origin the card was proposed on,
 *      else nothing is done ("the page changed");
 *   4. the page's half (page_action_page.js) is put in, in every frame, with
 *      chrome.scripting. Each frame says what it matches by the element's words;
 *      the one match, in whichever frame, gets the action. Only a refused
 *      injection falls back: once the step started it is never retried, and a
 *      failure then is "unknown_outcome". The page compares its own
 *      location.origin first. A page the browser refuses: the debugger path
 *      (page_action_cdp.js) does the same through background.js's own
 *      attach/send, and detaches afterwards unless something holds the tab.
 *      "not found", "ambiguous" and "submit_blocked" are answers, not
 *      failures: they never fall back. If both ways are refused the answer is
 *      "unavailable".
 *
 * The extension never submits a form: a click that could is not made
 * (submit_blocked). Nothing here reads the page's text back to the chat.
 *
 * After agent_cdp.js, chat_proposals.js, page_action_page.js and
 * page_action_cdp.js. install() takes {detach(tabId)}.
 */

(function (root) {
  "use strict";

  const PAGE_FILE = "page_action_page.js";
  let deps = {};

  const answer = (code, error, extra) => Object.assign({ ok: false, code, error }, extra);

  // A file: page has no origin to compare (every file is "null"), so it is never acted on.
  const originOf = (url) => {
    try {
      const u = new URL(url);
      return u.protocol === "http:" || u.protocol === "https:" ? u.origin : "";
    } catch {
      return "";
    }
  };

  /** isExtensionPage: one of this extension's own pages (the side panel), as vault_bridge.js. */
  function isExtensionPage(sender) {
    const base = chrome.runtime.getURL ? chrome.runtime.getURL("") : "";
    return !!base && String(sender.url || "").startsWith(base);
  }

  const UNKNOWN = "the page changed while the step ran; check the page";
  const unknown = () => answer("unknown_outcome", UNKNOWN);

  /** inject puts the page's half into every frame. Throws when the browser refuses the page: the only thing that falls back. */
  const inject = (tabId) => chrome.scripting.executeScript({ target: { tabId, allFrames: true }, files: [PAGE_FILE] });

  // Steps that put the person's words or choice into a page: not into a frame of another origin unasked.
  const FRAME_CONFIRMED = new Set(["type", "select", "check", "uncheck"]);
  const DEBUGGER_STEPS = new Set(["click", "double_click", "type"]);
  // Steps that need no element when none is named.
  const BARE = new Set(["scroll", "press_key"]);

  /**
   * call runs the step. Every frame says what it matches first (nothing is
   * done); the step runs only when the frames together hold exactly one, and
   * only in that frame. It is never retried: once it started, a failure
   * leaves the outcome unknown, and the top frame checks the origin itself
   * before it acts.
   */
  async function call(tabId, a, origin, confirmedFrame) {
    let frameId = 0;
    // The address the frame had when it was searched: a subframe is checked against it (its own origin is not the card's).
    let frameHref;
    if (!(BARE.has(a.action) && !a.target)) {
      let seen;
      try {
        seen = await chrome.scripting.executeScript({
          target: { tabId, allFrames: true },
          func: (action, target) => (globalThis.MonoPageAction ? globalThis.MonoPageAction.find(action, target) : null),
          args: [a.action, a.target],
        });
      } catch {
        return answer("unavailable", "the page could not be searched");
      }
      const hits = (seen || []).filter((r) => r && r.result && r.result.count > 0);
      // An exact name in any frame outranks a looser one in another.
      const tier = hits.some((r) => r.result.exact) ? hits.filter((r) => r.result.exact) : hits;
      const total = tier.reduce((n, r) => n + r.result.count, 0);
      if (total === 0) return answer("not_found", "not found");
      if (total > 1) return answer("ambiguous", `ambiguous (${total} matches)`);
      frameId = tier[0].frameId || 0;
      if (frameId) frameHref = tier[0].result.href || "";
      // Words typed, chosen or ticked in a frame from another site are the person's call: the card shows the frame first.
      const foreign = tier[0].result.origin;
      if (frameId && FRAME_CONFIRMED.has(a.action) && foreign !== origin && confirmedFrame !== foreign) {
        return answer("frame_confirm", "that field is in an iframe from another site", { frameOrigin: String(foreign || "") });
      }
    }
    try {
      const [res] = await chrome.scripting.executeScript({
        target: { tabId, frameIds: [frameId] },
        func: (action, target, text, expected, href) => globalThis.MonoPageAction.run(action, target, text, expected, href),
        args: frameId ? [a.action, a.target, a.text, origin, frameHref] : [a.action, a.target, a.text, origin],
      });
      return res && res.result ? Object.assign({ method: "script" }, frameId ? { frame: frameId } : {}, res.result) : unknown();
    } catch {
      return unknown();
    }
  }

  /** viaDebugger: the same through CDP; the tab is let go afterwards unless something holds it. */
  async function viaDebugger(tabId, a, origin) {
    try {
      return await root.MonoPageActionCdp.run(tabId, Object.assign({ origin }, a));
    } finally {
      if (!root.MonoAgentCdp.holds(tabId) && deps.detach) {
        try {
          await deps.detach(tabId);
        } catch {
          // already gone
        }
      }
    }
  }

  async function run(msg) {
    const kind = root.MonoChatProposals && root.MonoChatProposals.KINDS.action;
    const p = (msg && msg.proposal) || {};
    const a = kind ? kind.fields(p) : null;
    if (!a || !kind.valid(a)) return answer("invalid", "that is not an action I can do");
    if (a.action === "open_url") {
      await chrome.tabs.create({ url: a.url, active: true });
      return { ok: true, method: "tab" };
    }
    const tab = msg.tab || {};
    if (!Number.isInteger(tab.tabId) || tab.tabId <= 0) return answer("invalid", "there is no page to act on");
    let live = null;
    try {
      live = await chrome.tabs.get(tab.tabId);
    } catch {
      return answer("page_changed", "the page changed: that tab is gone");
    }
    const was = originOf(tab.url);
    if (!was || originOf(live && live.url) !== was) return answer("page_changed", "the page changed since this was suggested");

    let scriptError = "";
    try {
      await inject(tab.tabId);
    } catch (err) {
      scriptError = (err && err.message) || String(err);
    }
    if (!scriptError) return call(tab.tabId, a, was, typeof msg.frameOrigin === "string" ? msg.frameOrigin : "");
    // The debugger path knows three steps; any other would be done as a click.
    if (!DEBUGGER_STEPS.has(a.action)) return answer("unavailable", `this page cannot be acted on this way (${scriptError}); the coder can drive it`);
    try {
      return Object.assign({ method: "debugger" }, await viaDebugger(tab.tabId, a, was));
    } catch (err) {
      if (err && err.started) return Object.assign({ method: "debugger" }, unknown());
      return answer("unavailable", `this page cannot be acted on (${scriptError}; ${(err && err.message) || err})`);
    }
  }

  function install(d) {
    deps = d || {};
    chrome.runtime.onMessage.addListener((msg, sender, respond) => {
      if (!msg || msg.type !== "page_action_run") return false;
      // Only this extension's own pages: a web page cannot drive another.
      if (!sender || sender.id !== chrome.runtime.id || !isExtensionPage(sender)) return false;
      run(msg).then(respond, (err) => respond(answer("unavailable", (err && err.message) || String(err))));
      return true;
    });
  }

  root.MonoPageActionBridge = { install, run };
})(globalThis);
