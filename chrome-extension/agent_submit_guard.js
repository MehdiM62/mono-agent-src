/**
 * MonoAgent Bridge — submit_guard, the worker's half (plan C1a)
 *
 * Before an agent acts on a page whose submits need a person's approval, the
 * CLI arms this tab's guard; every submit the page then tries is stopped in
 * the page (agent_submit_page.js) and recorded here, where the CLI reads it
 * (status) and turns it into an approval item. Once approved, allow_once
 * lets exactly the next submit through.
 *
 *   arm         inject the guard into every frame, and into every document
 *               the tab commits from now on (webNavigation.onCommitted)
 *   disarm      stop blocking; the guards stay in their pages, inert
 *   allow_once  the next submit passes, in the frame the last blocked one
 *               came from (else the top frame)
 *   status      what all four answer: {armed, blocked:[{seq,ts,url,form}]}
 *
 * The armed tabs and their blocked submits are kept in chrome.storage.session
 * as well as here, so a worker restart neither forgets to re-arm a tab on its
 * next navigation nor loses an attempt the CLI has not read yet. A browser
 * restart clears them. A tab keeps its last MAX_BLOCKED attempts.
 *
 * Node-tested against a fake chrome (agent_submit_guard.test.mjs).
 */

(function (root) {
  "use strict";

  const ACTIONS = ["arm", "disarm", "allow_once", "status"];
  const STORE_KEY = "agentSubmitGuard";
  // The guard's handle on the page's window (see installGuard).
  const GUARD_KEY = "__monoagentSubmitGuard";
  const MESSAGE = "agent_submit_blocked";
  const MAX_BLOCKED = 50;

  let tabs = {}; // String(tabId) -> { armed, seq, blocked: [{seq, ts, url, form, frameId}] }
  let ready = null;

  function load() {
    if (!ready) {
      ready = Promise.resolve()
        .then(() => chrome.storage.session.get(STORE_KEY))
        .then(
          (got) => {
            const saved = got && got[STORE_KEY];
            if (saved && typeof saved === "object") tabs = Object.assign(saved, tabs);
          },
          () => {}
        );
    }
    return ready;
  }

  function save() {
    try {
      chrome.storage.session.set({ [STORE_KEY]: tabs }).catch(() => {});
    } catch {
      // no session storage: the state lives as long as this worker
    }
  }

  function view(st) {
    return {
      armed: !!st.armed,
      blocked: st.blocked.map((b) => ({ seq: b.seq, ts: b.ts, url: b.url, form: b.form })),
    };
  }

  function nonce() {
    const bytes = new Uint8Array(12);
    crypto.getRandomValues(bytes);
    return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  }

  /** inject puts the relay, then the guard, into the target frames. */
  async function inject(tabId, frames, immediately) {
    const page = root.MonoAgentSubmitPage;
    const target = Object.assign({ tabId }, frames);
    const event = `monoagent-submit-${nonce()}`;
    await chrome.scripting.executeScript({ target, func: page.installRelay, args: [event], injectImmediately: !!immediately });
    const results = await chrome.scripting.executeScript({
      target,
      world: "MAIN",
      func: page.installGuard,
      args: [GUARD_KEY, event],
      injectImmediately: !!immediately,
    });
    return (results || []).some((r) => r && r.result === true);
  }

  /** control runs one guard action in the target frames; true when a guard took it. */
  async function control(tabId, frames, action) {
    const results = await chrome.scripting.executeScript({
      target: Object.assign({ tabId }, frames),
      world: "MAIN",
      args: [GUARD_KEY, action],
      func: (key, a) => {
        const guard = window[key];
        return !!guard && guard.control(a) === true;
      },
    });
    return (results || []).some((r) => r && r.result === true);
  }

  /** handle answers submit_guard. */
  async function handle(params) {
    const tabId = root.MonoAgentCdp.tabOf(params, "submit_guard");
    const action = params.action;
    if (!ACTIONS.includes(action)) throw new Error(`submit_guard: action must be one of ${ACTIONS.join(", ")}`);
    await load();
    const st = tabs[tabId] || { armed: false, seq: 0, blocked: [] };
    if (action === "arm") {
      if (!(await inject(tabId, { allFrames: true }, false))) {
        throw new Error("submit_guard: the guard could not be installed in this page");
      }
      st.armed = true;
      tabs[tabId] = st;
      save();
    } else if (action === "disarm") {
      st.armed = false;
      if (tabs[tabId]) save();
      await control(tabId, { allFrames: true }, "disarm").catch(() => false);
    } else if (action === "allow_once") {
      if (!st.armed) throw new Error("submit_guard: the guard is not armed on this tab");
      const last = st.blocked[st.blocked.length - 1];
      const frameId = last && Number.isInteger(last.frameId) ? last.frameId : 0;
      let ok = await control(tabId, { frameIds: [frameId] }, "allow_once").catch(() => false);
      if (!ok && frameId !== 0) ok = await control(tabId, { frameIds: [0] }, "allow_once").catch(() => false);
      if (!ok) throw new Error("submit_guard: no guard in this page to let a submit through (arm it again)");
    }
    return view(st);
  }

  const str = (v, n) => (typeof v === "string" ? v.slice(0, n) : "");

  /** normalize re-checks a report from the page: it is page-written data. */
  function normalize(detail) {
    let d;
    try {
      d = JSON.parse(detail);
    } catch {
      return null;
    }
    if (!d || typeof d !== "object") return null;
    const f = d.form && typeof d.form === "object" ? d.form : {};
    const fields = Array.isArray(f.fields) ? f.fields.slice(0, 100) : [];
    return {
      url: str(d.url, 2048),
      form: {
        action: str(f.action, 2048),
        method: str(f.method, 16),
        fields: fields.map((x) => ({
          name: str(x && x.name, 200),
          type: str(x && x.type, 40),
          label: str(x && x.label, 200),
          value: str(x && x.value, 500),
        })),
        submitter: str(f.submitter, 1000),
        selector: str(f.selector, 1000),
      },
    };
  }

  async function record(tabId, frameId, frameUrl, detail) {
    await load();
    const st = tabs[tabId];
    if (!st || !st.armed) return;
    const report = normalize(detail);
    if (!report) return;
    st.seq += 1;
    st.blocked.push({
      seq: st.seq,
      ts: Date.now(),
      url: str(frameUrl, 2048) || report.url,
      form: report.form,
      frameId: Number.isInteger(frameId) ? frameId : 0,
    });
    if (st.blocked.length > MAX_BLOCKED) st.blocked.shift();
    save();
  }

  async function committed(details) {
    await load();
    const st = tabs[details.tabId];
    if (!st || !st.armed) return;
    // As early as Chrome allows: before the page's own scripts, usually.
    await inject(details.tabId, { frameIds: [details.frameId] }, true).catch(() => false);
  }

  async function removed(tabId) {
    await load();
    if (!tabs[tabId]) return;
    delete tabs[tabId];
    save();
  }

  /** install registers the three listeners; called once, at worker start. */
  function install() {
    chrome.webNavigation.onCommitted.addListener((details) => {
      committed(details).catch(() => {});
    });
    chrome.runtime.onMessage.addListener((msg, sender) => {
      if (!msg || msg.type !== MESSAGE) return false;
      const tabId = sender && sender.tab && sender.tab.id;
      if (!Number.isInteger(tabId) || (sender.id && sender.id !== chrome.runtime.id)) return false;
      record(tabId, sender.frameId, sender.url, msg.detail).catch(() => {});
      return false;
    });
    chrome.tabs.onRemoved.addListener((tabId) => {
      removed(tabId).catch(() => {});
    });
    load();
  }

  root.MonoAgentSubmitGuard = { install, handle, normalize, GUARD_KEY };
})(globalThis);
