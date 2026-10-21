// Doing a chat card's page action (page_action_bridge.js): only the side
// panel asks, the tab must still be on the origin the card was proposed on,
// chrome.scripting goes first and the debugger only when the browser refuses
// the page, and what a page answers (not found, ambiguous, submit blocked)
// is an answer, never a reason to try the other way.
// `CHROME_PATH=/nonexistent node --test chrome-extension/page_action_bridge.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const PANEL = { id: "ext", url: "chrome-extension://ext/sidepanel.html" };
const PAGE = { id: "ext", url: "https://a.example/page", tab: { id: 5 } };
const TAB = { tabId: 5, url: "https://a.example/files" };

function world(o = {}) {
  const calls = { scripts: [], created: [], detached: [], cdp: [], cdpRuns: [] };
  const listeners = [];
  const chrome = {
    runtime: { id: "ext", getURL: (p) => `chrome-extension://ext/${p}`, onMessage: { addListener: (fn) => listeners.push(fn) } },
    scripting: {
      executeScript: async (opts) => {
        calls.scripts.push(opts);
        if (o.scriptFails) throw new Error("Cannot access contents of url \"chrome://extensions\"");
        if (opts.files) return [{ result: undefined }];
        if (opts.target.allFrames) return o.frames || [{ frameId: 0, result: { count: 1, exact: true, matched: "x", held: "" } }];
        if (o.callFails) throw new Error("Frame with ID 0 was removed.");
        if (o.noResult) return [];
        return [{ result: o.pageReply || { ok: true } }];
      },
    },
    tabs: {
      get: async (id) => {
        if (o.tabGone) throw new Error("No tab with id");
        return { id, url: o.liveUrl || "https://a.example/files/other" };
      },
      create: async (opts) => (calls.created.push(opts), { id: 9 }),
    },
  };
  const g = loadExtensionScripts(["agent_cdp.js", "chat_proposals_act.js", "chat_proposals.js", "page_action_page.js", "page_action_cdp.js", "page_action_bridge.js"], { chrome });
  g.MonoAgentCdp.install({
    attach: async (tabId) => calls.cdp.push(["attach", tabId]),
    cdp: async (tabId, method) => (calls.cdp.push([method]), {}),
    onDebuggerEvent: () => {},
    onDebuggerDetach: () => {},
  });
  if (o.debuggerFails) g.MonoAgentCdp.send = async () => { throw new Error("debugger attach: Cannot attach to this target"); };
  g.MonoPageActionCdp.run = async (tabId, act) => {
    calls.cdpRuns.push([tabId, act]);
    if (o.debuggerFails) throw new Error("debugger attach: Cannot attach to this target");
    if (o.debuggerStarted) throw Object.assign(new Error("Target closed"), { started: true });
    return o.cdpReply || { ok: true };
  };
  if (o.held) g.MonoAgentCdp.hold(5, "watch");
  g.MonoPageActionBridge.install({ detach: async (tabId) => calls.detached.push(tabId) });
  const send = (msg, sender = PANEL) =>
    new Promise((resolve) => {
      const handled = listeners.map((fn) => fn(msg, sender, resolve));
      if (!handled.some(Boolean)) resolve(undefined);
    });
  return { calls, send };
}

const ask = (proposal, tab = TAB) => ({ type: "page_action_run", tab, proposal: Object.assign({ id: "toolu_a", kind: "action", action: "double_click", target: "CV - Morteza Nokhodian 2025 Apr" }, proposal) });

test("Run puts the page half in, then does the action there: script first", async () => {
  const w = world();
  assert.deepEqual(await w.send(ask()), { method: "script", ok: true });
  assert.deepEqual(w.calls.scripts[0], { target: { tabId: 5, allFrames: true }, files: ["page_action_page.js"] });
  assert.deepEqual(w.calls.scripts[2].args, ["double_click", "CV - Morteza Nokhodian 2025 Apr", "", "https://a.example"]);
  assert.equal(w.calls.scripts[2].target.tabId, 5);
  assert.deepEqual([w.calls.cdpRuns.length, w.calls.cdp.length, w.calls.detached.length], [0, 0, 0], "the debugger is not touched");
});

test("only the side panel may ask", async () => {
  const w = world();
  assert.equal(await w.send(ask(), PAGE), undefined, "a page cannot send one");
  assert.equal(await w.send(ask(), { id: "other-ext", url: "chrome-extension://ext/sidepanel.html" }), undefined);
  assert.equal(await w.send({ type: "something_else" }), undefined);
  assert.deepEqual(w.calls.scripts, []);
});

test("the card is checked again: an unknown action or a bad address does nothing", async () => {
  const w = world();
  for (const bad of [{ action: "delete" }, { target: "" }, { action: "type", text: "" }, { action: "open_url", url: "javascript:alert(1)" }, { action: "open_url", url: "file:///etc/passwd" }]) {
    const out = await w.send(ask(bad));
    assert.deepEqual([out.ok, out.code], [false, "invalid"], JSON.stringify(bad));
  }
  assert.deepEqual([w.calls.scripts.length, w.calls.created.length], [0, 0]);
});

test("open_url opens a NEW tab, http or https, and touches no page", async () => {
  const w = world({ liveUrl: "https://elsewhere.example/" });
  assert.deepEqual(await w.send(ask({ action: "open_url", target: "", url: "https://b.example/x?y=1" })), { ok: true, method: "tab" });
  assert.deepEqual(w.calls.created, [{ url: "https://b.example/x?y=1", active: true }]);
  assert.deepEqual([w.calls.scripts.length, w.calls.cdp.length], [0, 0]);
});

test("a tab that is on another origin than the card's is not acted on: the page changed", async () => {
  for (const o of [{ liveUrl: "https://evil.example/files" }, { liveUrl: "http://a.example/files" }, { liveUrl: "https://a.example:8443/" }, { liveUrl: "chrome://extensions" }, { tabGone: true }]) {
    const w = world(o);
    const out = await w.send(ask());
    assert.deepEqual([out.ok, out.code], [false, "page_changed"], JSON.stringify(o));
    assert.match(out.error, /page changed/);
    assert.deepEqual([w.calls.scripts.length, w.calls.cdpRuns.length, w.calls.cdp.length], [0, 0, 0], "nothing was injected, nothing attached");
  }
  const noTab = await world().send(ask({}, { tabId: 0, url: "https://a.example/" }));
  assert.equal(noTab.ok, false);
  const noOrigin = await world().send(ask({}, { tabId: 5, url: "about:blank" }));
  assert.equal(noOrigin.code, "page_changed", "a card with no origin to compare is not run");
});

test("the same site under another path is still the page", async () => {
  const w = world({ liveUrl: "https://a.example/files/folder/2" });
  assert.equal((await w.send(ask())).ok, true);
});

test("the debugger is used only when the browser refuses the page, then detached", async () => {
  const w = world({ scriptFails: true });
  assert.deepEqual(await w.send(ask()), { method: "debugger", ok: true });
  assert.equal(w.calls.scripts.length, 1, "the script path was tried first and refused");
  assert.deepEqual(w.calls.cdpRuns, [[5, { origin: "https://a.example", action: "double_click", target: "CV - Morteza Nokhodian 2025 Apr", text: "", url: "" }]]);
  assert.deepEqual(w.calls.detached, [5], "let go afterwards");
});

test("a tab something holds (a coder watch) is not detached", async () => {
  const w = world({ scriptFails: true, held: true });
  assert.equal((await w.send(ask())).method, "debugger");
  assert.deepEqual(w.calls.detached, []);
});

test("what the page answers is final: not found, ambiguous and submit blocked never fall back", async () => {
  for (const reply of [
    { ok: false, code: "not_found", error: "not found" },
    { ok: false, code: "ambiguous", error: "ambiguous (3 matches)" },
    { ok: false, code: "submit_blocked", error: "that would submit a form, so it was not clicked: press that yourself" },
  ]) {
    const w = world({ pageReply: reply });
    assert.deepEqual(await w.send(ask()), Object.assign({ method: "script" }, reply));
    assert.deepEqual([w.calls.cdpRuns.length, w.calls.cdp.length], [0, 0]);
  }
  const w = world({ scriptFails: true, cdpReply: { ok: false, code: "ambiguous", error: "ambiguous (2 matches)" } });
  assert.deepEqual(await w.send(ask()), { method: "debugger", ok: false, code: "ambiguous", error: "ambiguous (2 matches)" });
});

test("when both ways are refused (chrome:// and store pages) the answer says so, for the card's Hand to coder", async () => {
  const w = world({ scriptFails: true, debuggerFails: true });
  const out = await w.send(ask());
  assert.deepEqual([out.ok, out.code], [false, "unavailable"]);
  assert.match(out.error, /cannot be acted on/);
  assert.match(out.error, /chrome:\/\/extensions/);
  assert.deepEqual(w.calls.detached, [5], "a failed attempt is let go too");
});

test("typed text rides to the page as the card's text", async () => {
  const w = world();
  await w.send(ask({ action: "type", target: "Search", text: "annual report" }));
  assert.deepEqual(w.calls.scripts[2].args, ["type", "Search", "annual report", "https://a.example"]);
});

test("only a refused injection falls back: once the step started, a failure is an unknown outcome and is never retried", async () => {
  for (const o of [{ callFails: true }, { noResult: true }]) {
    const w = world(o);
    const out = await w.send(ask());
    assert.deepEqual(out, { ok: false, code: "unknown_outcome", error: "the page changed while the step ran; check the page" }, JSON.stringify(o));
    assert.equal(w.calls.scripts.length, 3, "injected once, searched once, called once");
    assert.deepEqual([w.calls.cdpRuns.length, w.calls.cdp.length, w.calls.detached.length], [0, 0, 0], "the debugger never repeats it");
  }
});

test("the expected origin goes into the page's call, which compares its own location.origin", async () => {
  const w = world({ pageReply: { ok: false, code: "page_changed", error: "the page changed since this was suggested" } });
  const out = await w.send(ask());
  assert.deepEqual([out.method, out.code], ["script", "page_changed"]);
  assert.equal(w.calls.scripts[2].args[3], "https://a.example");
});

test("a debugger step that failed after input was sent is an unknown outcome, not unavailable", async () => {
  const w = world({ scriptFails: true, debuggerStarted: true });
  assert.deepEqual(await w.send(ask()), { method: "debugger", ok: false, code: "unknown_outcome", error: "the page changed while the step ran; check the page" });
  assert.deepEqual(w.calls.detached, [5]);
});

test("a file: page has no origin to compare, so it is never acted on", async () => {
  for (const [card, live] of [["file:///home/a/one.html", "file:///home/a/one.html"], ["file:///home/a/one.html", "file:///home/a/two.html"]]) {
    const w = world({ liveUrl: live });
    const out = await w.send(ask({}, { tabId: 5, url: card }));
    assert.deepEqual([out.ok, out.code], [false, "page_changed"], `${card} -> ${live}`);
    assert.deepEqual([w.calls.scripts.length, w.calls.cdp.length], [0, 0]);
  }
});

test("every frame is searched; the step runs only in the frame that holds the one match", async () => {
  const w = world({ frames: [{ frameId: 0, result: { count: 0, exact: false, matched: "", held: "" } }, { frameId: 7, result: { count: 1, exact: true, matched: "GBP 200", held: "", origin: "https://a.example", href: "https://a.example/q" } }] });
  const out = await w.send(ask({ action: "check", target: "GBP 200" }));
  assert.deepEqual([out.ok, out.frame], [true, 7]);
  assert.deepEqual(w.calls.scripts[1].target, { tabId: 5, allFrames: true });
  assert.deepEqual(w.calls.scripts[2].target, { tabId: 5, frameIds: [7] });
});

test("a subframe's step carries the address it was found at; the top frame's does not", async () => {
  const w = world({ frames: [{ frameId: 0, result: { count: 0, exact: false, matched: "", held: "" } }, { frameId: 7, result: { count: 1, exact: true, matched: "GBP 200", held: "", origin: "https://f.example", href: "https://f.example/q" } }] });
  await w.send(ask({ action: "click", target: "GBP 200" }));
  assert.deepEqual(w.calls.scripts[2].args.slice(3), ["https://a.example", "https://f.example/q"], "the card's origin, then the frame's address");
  const top = world({ frames: [{ frameId: 0, result: { count: 1, exact: true, matched: "x", held: "", origin: "https://a.example", href: "https://a.example/files" } }] });
  await top.send(ask());
  assert.equal(top.calls.scripts[2].args.length, 4);
});

test("what is held in the found frame is reported, not hidden by the bridge", async () => {
  const w = world({ pageReply: { ok: false, code: "page_changed", error: "the page changed since this was suggested" }, frames: [{ frameId: 4, result: { count: 1, exact: true, matched: "x", held: "", origin: "https://f.example", href: "https://f.example/" } }] });
  const out = await w.send(ask());
  assert.deepEqual([out.ok, out.code, out.frame], [false, "page_changed", 4]);
});

test("one match in each of two frames is ambiguous, and nothing is done; an exact name beats a loose one", async () => {
  const two = world({ frames: [{ frameId: 0, result: { count: 1, exact: true, matched: "a", held: "" } }, { frameId: 3, result: { count: 1, exact: true, matched: "a", held: "" } }] });
  assert.deepEqual(await two.send(ask()), { ok: false, code: "ambiguous", error: "ambiguous (2 matches)" });
  assert.equal(two.calls.scripts.length, 2, "no step was run");
  const mixed = world({ frames: [{ frameId: 0, result: { count: 2, exact: false, matched: "a", held: "" } }, { frameId: 3, result: { count: 1, exact: true, matched: "a", held: "" } }] });
  assert.equal((await mixed.send(ask())).frame, 3);
});

test("a step with no element (scroll, a key) is not searched for", async () => {
  const w = world();
  assert.equal((await w.send(ask({ action: "scroll", target: "", text: "down" }))).ok, true);
  assert.equal(w.calls.scripts.length, 2, "injected, then run: no search");
});

test("a refused page is not driven by the debugger for a step it cannot do: it is not a click", async () => {
  const w = world({ scriptFails: true });
  for (const action of ["check", "select", "hover", "scroll", "press_key"]) {
    const out = await w.send(ask({ action, target: action === "scroll" || action === "press_key" ? "" : "Country", text: action === "scroll" ? "down" : action === "press_key" ? "Tab" : "Ireland" }));
    assert.equal(out.code, "unavailable", action);
  }
  assert.deepEqual([w.calls.cdpRuns.length, w.calls.cdp.length], [0, 0]);
});

const FOREIGN = [{ frameId: 7, result: { count: 1, exact: true, matched: "Email", held: "", origin: "https://ads.example", href: "https://ads.example/f" } }];

test("typing, choosing or ticking in a frame of another origin is refused until the person confirms that frame's origin", async () => {
  for (const action of ["type", "select", "check", "uncheck"]) {
    const w = world({ frames: FOREIGN });
    const out = await w.send(ask({ action, target: "Email", text: "x" }));
    assert.deepEqual([out.ok, out.code, out.frameOrigin], [false, "frame_confirm", "https://ads.example"], action);
    assert.equal(w.calls.scripts.length, 2, "injected and searched; nothing was done");
    const other = world({ frames: FOREIGN });
    const wrong = await other.send(Object.assign(ask({ action, target: "Email", text: "x" }), { frameOrigin: "https://else.example" }));
    assert.equal(wrong.code, "frame_confirm", "confirming another origin is not confirming this one");
    const yes = world({ frames: FOREIGN });
    const done = await yes.send(Object.assign(ask({ action, target: "Email", text: "x" }), { frameOrigin: "https://ads.example" }));
    assert.deepEqual([done.ok, done.frame], [true, 7], action);
  }
});

test("a click in another origin's frame, and typing in the tab's own origin frame, need no confirmation", async () => {
  assert.equal((await world({ frames: FOREIGN }).send(ask({ action: "click", target: "Email" }))).ok, true);
  const same = [{ frameId: 7, result: { count: 1, exact: true, matched: "Email", held: "", origin: "https://a.example", href: "https://a.example/f" } }];
  assert.equal((await world({ frames: same }).send(ask({ action: "type", target: "Email", text: "x" }))).ok, true);
});
