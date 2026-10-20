/**
 * MonoAgent Bridge — doing what a chat card proposed on the page (the worker's half)
 *
 *   page_action_run {tab: {tabId, url}, proposal: {action, target?, text?, url?}}
 *     -> {ok:true, method: "script"|"debugger"|"tab"}
 *      | {ok:false, code, error, method?}
 *        codes: invalid, page_changed, not_found, ambiguous, submit_blocked,
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
 *   4. the page's half (page_action_page.js) is put in with chrome.scripting
 *      and finds the element by its words and does the action. A page the
 *      browser refuses throws, and only then the debugger path
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

  const originOf = (url) => {
    try {
      const u = new URL(url);
      return u.protocol === "http:" || u.protocol === "https:" || u.protocol === "file:" ? u.origin : "";
    } catch {
      return "";
    }
  };

  /** isExtensionPage: one of this extension's own pages (the side panel), as vault_bridge.js. */
  function isExtensionPage(sender) {
    const base = chrome.runtime.getURL ? chrome.runtime.getURL("") : "";
    return !!base && String(sender.url || "").startsWith(base);
  }

  /** viaScript: the page's half injected, then run. Throws when the browser refuses the page. */
  async function viaScript(tabId, a) {
    await chrome.scripting.executeScript({ target: { tabId }, files: [PAGE_FILE] });
    const [res] = await chrome.scripting.executeScript({
      target: { tabId },
      func: (action, target, text) => globalThis.MonoPageAction.run(action, target, text),
      args: [a.action, a.target, a.text],
    });
    if (!res || !res.result) throw new Error("the page gave no answer");
    return res.result;
  }

  /** viaDebugger: the same through CDP; the tab is let go afterwards unless something holds it. */
  async function viaDebugger(tabId, a) {
    try {
      return await root.MonoPageActionCdp.run(tabId, a);
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
      return Object.assign({ method: "script" }, await viaScript(tab.tabId, a));
    } catch (err) {
      scriptError = (err && err.message) || String(err);
    }
    try {
      return Object.assign({ method: "debugger" }, await viaDebugger(tab.tabId, a));
    } catch (err) {
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
