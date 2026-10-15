/**
 * MonoAgent Bridge — the form filler's worker half
 *
 *   form_fill {profile, windowId}  ->  {ok, filled, found, source?}
 *
 * Scans the active tab's form fields (form_page.js), asks the bridge which of
 * them the profile's own documents can answer (`form.fill`), and writes the
 * answers into the page. It never submits: once a field was filled, the page
 * asks the person to review the form and press Submit themselves (decision 4,
 * MonoCoderBridge.promptUserToSubmit). Runtime and model are the panel's
 * "AI for summaries" choice, as for chat.
 *
 * After ask.js, chat_core.js and summary_ai.js; recall_bridge.js installs
 * ask.js itself, so install() here never does.
 */

(function (root) {
  "use strict";

  const PAGE_FILE = "form_page.js";
  const FILL_TIMEOUT_MS = 180000;
  const SUBMIT_MESSAGE = "Review the form and press Submit yourself";
  let running = false;

  async function activeTab(msg) {
    const query = Number.isInteger(msg.windowId) ? { active: true, windowId: msg.windowId } : { active: true, lastFocusedWindow: true };
    const [tab] = await chrome.tabs.query(query);
    return tab || null;
  }

  const inPage = async (tabId, func, args) => {
    const [res] = await chrome.scripting.executeScript({ target: { tabId }, func, args: args || [] });
    return res && res.result;
  };

  async function fill(msg) {
    const profile = typeof msg.profile === "string" && root.MonoChatCore.validProfile(msg.profile) ? msg.profile.trim() : "";
    if (!profile) throw new Error("choose a profile first");
    if (running) throw new Error("a form is already being filled");
    const tab = await activeTab(msg);
    if (!tab || !tab.id) throw new Error("there is no page to fill");
    running = true;
    try {
      try {
        await chrome.scripting.executeScript({ target: { tabId: tab.id }, files: [PAGE_FILE] });
      } catch {
        throw new Error("this page cannot be filled (the browser does not let extensions in)");
      }
      const scan = await inPage(tab.id, () => globalThis.MonoFormPage.scan());
      const asked = ((scan && scan.fields) || []).filter((f) => !f.filled);
      if (!asked.length) {
        await inPage(tab.id, () => globalThis.MonoFormPage.apply([]));
        return { ok: true, filled: 0, found: 0, none: true };
      }
      const params = { profile, url: scan.url, fields: asked.map(({ filled, ...rest }) => rest) };
      let ai = {};
      try {
        ai = (await root.MonoSummaryAI.sticky(chrome.storage.local)) || {};
      } catch {
        ai = {};
      }
      if (ai.runtime) params.runtime = ai.runtime;
      if (ai.runtime && ai.model) params.model = ai.model;
      let data;
      try {
        data = await root.MonoAsk.request("form.fill", params, { timeoutMs: FILL_TIMEOUT_MS, idleTimeoutMs: FILL_TIMEOUT_MS });
      } catch (err) {
        await inPage(tab.id, () => globalThis.MonoFormPage.apply([]));
        throw err;
      }
      const values = (data && Array.isArray(data.values) ? data.values : []).filter((v) => v && typeof v.id === "string" && typeof v.value === "string");
      const done = await inPage(tab.id, (vals) => globalThis.MonoFormPage.apply(vals), [values.map(({ id, value }) => ({ id, value }))]);
      // No selector: the banner finds the form the last filled field is in.
      if (done && done.filled > 0 && root.MonoCoderBridge) await root.MonoCoderBridge.promptUserToSubmit(tab.id, { message: SUBMIT_MESSAGE });
      const sources = [...new Set(values.map((v) => v.source).filter(Boolean))];
      return { ok: true, filled: (done && done.filled) || 0, found: asked.length, sources };
    } finally {
      running = false;
    }
  }

  function install() {
    chrome.runtime.onMessage.addListener((msg, sender, respond) => {
      if (!msg || msg.type !== "form_fill") return false;
      fill(msg)
        .then(respond)
        .catch((err) => respond({ ok: false, error: err.message || String(err), code: err.code }));
      return true;
    });
  }

  root.MonoForm = { install, fill };
})(globalThis);
