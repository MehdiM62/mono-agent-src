// What a click on an action or a coder card does (sidepanel_chat_cards.js):
// Run asks the worker to act on the page the card was made for; a failed
// action offers Hand to coder; Hand to coder queues a coder chat with the
// card's goal and why for this tab and NOTHING else (no page text), and the
// card then says it is queued, not started.
// `CHROME_PATH=/nonexistent node --test chrome-extension/sidepanel_chat_cards_act.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const { MonoChatCards: C, MonoChatProposals: P } = loadExtensionScripts(["private_net.js", "chat_proposals_act.js", "chat_proposals.js", "sidepanel_chat_cards.js"]);

const PAGE = { tabId: 5, url: "https://drive.example/folder/1" };
const N = (c) => c.repeat(16);

function world({ reply, acted } = {}) {
  const chat = {
    messages: [{
      id: "m1", role: "assistant", text: "", tools: [],
      proposals: [
        { id: "a1", kind: "action", action: "double_click", target: "CV - Morteza Nokhodian 2025 Apr", text: "", url: "", untrustedPage: true, state: "pending", feedback: "", nonce: N("a"), page: PAGE },
        { id: "a2", kind: "action", action: "type", target: "Search", text: "annual report", url: "", untrustedPage: false, state: "pending", feedback: "", nonce: N("b"), page: PAGE },
        { id: "a3", kind: "action", action: "open_url", target: "", text: "", url: "https://b.example/x", untrustedPage: false, state: "pending", feedback: "", nonce: N("c") },
        { id: "c1", kind: "coder", goal: "Rename the files", why: "Needs the shell", untrustedPage: true, state: "pending", feedback: "", nonce: N("d"), page: PAGE },
      ],
    }],
  };
  const calls = [];
  let page = PAGE;
  let seq = 1;
  let answer = reply || (() => ({ ok: true, method: "script" }));
  const cards = C.create({
    chat: () => chat,
    ask: async (m) => (calls.push(m), answer(m)),
    profile: () => "p-work",
    current: () => seq,
    save: async () => {},
    draw: () => {},
    record: null,
    page: () => page,
    acted,
  });
  const card = (id) => chat.messages[0].proposals.find((p) => p.id === id);
  return { chat, calls, cards, card, setPage: (p) => (page = p), setAnswer: (fn) => (answer = fn), switchProfile: () => seq++ };
}

test("nothing is sent until Run; then the worker is asked once, with the card's own words and the tab it was made on", async () => {
  const w = world();
  assert.deepEqual(w.calls, []);
  await w.cards.run(N("a"));
  assert.deepEqual(w.calls, [{
    type: "page_action_run",
    tab: PAGE,
    proposal: { id: "a1", kind: "action", action: "double_click", target: "CV - Morteza Nokhodian 2025 Apr", text: "", url: "", nonce: N("a") },
  }]);
  assert.deepEqual([w.card("a1").state, w.card("a1").feedback], ["done", "Done."]);
  await w.cards.run(N("a"));
  assert.equal(w.calls.length, 1, "a done card runs no more");
});

test("a click on a card that is not Run's kind does nothing", async () => {
  const w = world();
  await w.cards.run(N("d"));
  await w.cards.run("nope");
  assert.deepEqual(w.calls, []);
});

test("confirm sends an action card to Run and a coder card to Hand to coder", async () => {
  const w = world({ reply: (m) => (m.type === "coder_queue" ? { ok: true, item: {} } : { ok: true }) });
  await w.cards.confirm(N("a"));
  await w.cards.confirm(N("d"));
  assert.deepEqual(w.calls.map((m) => m.type), ["page_action_run", "coder_queue"]);
});

test("the person's page moving to another site refuses the card, and nothing is sent", async () => {
  const w = world();
  w.setPage({ tabId: 5, url: "https://evil.example/" });
  await w.cards.run(N("a"));
  assert.deepEqual(w.calls, []);
  assert.equal(w.card("a1").state, "pending");
  assert.match(w.card("a1").feedback, /suggested for drive\.example\/folder\/1, but the panel shows evil\.example\/.*press Run again/);
  w.setPage({ tabId: 6, url: PAGE.url });
  await w.cards.run(N("a"));
  assert.deepEqual(w.calls, [], "another tab is another page");
  w.setPage({ tabId: 5, url: "https://drive.example/folder/2#x" });
  await w.cards.run(N("a"));
  assert.equal(w.calls.length, 1, "the same site under another path is still the page");
});

test("an action with no page to be run on is not sent, but open_url needs none", async () => {
  const w = world();
  w.card("a1").page = undefined;
  await w.cards.run(N("a"));
  assert.deepEqual(w.calls, []);
  assert.match(w.card("a1").feedback, /not suggested for a page/);
  await w.cards.run(N("c"));
  assert.equal(w.calls[0].type, "page_action_run");
  assert.equal(w.calls[0].tab, null);
  assert.equal(w.calls[0].proposal.url, "https://b.example/x");
  assert.equal(w.card("a3").feedback, "Opened in a new tab.");
});

test("a failed run says why and offers Hand to coder; a held-back submit offers only Dismiss", async () => {
  const w = world({ reply: () => ({ ok: false, code: "unavailable", error: "this page cannot be acted on (nope)" }) });
  await w.cards.run(N("a"));
  const failed = w.card("a1");
  assert.deepEqual([failed.state, failed.code, failed.feedback], ["failed", "unavailable", "Not done: this page cannot be acted on (nope)"]);
  assert.deepEqual(P.view(failed).actions, ["run", "coder", "dismiss"]);

  w.setAnswer(() => ({ ok: false, code: "submit_blocked", error: "that would submit a form, so it was not clicked: press that button yourself" }));
  await w.cards.run(N("b"));
  const blocked = w.card("a2");
  assert.deepEqual([blocked.state, blocked.code], ["failed", "submit_blocked"]);
  assert.match(blocked.feedback, /press that button yourself/);
  assert.deepEqual(P.view(blocked).actions, ["dismiss"]);
  const before = w.calls.length;
  await w.cards.run(N("b"));
  await w.cards.coder(N("b"));
  assert.equal(w.calls.length, before, "neither Run again nor a handoff for a held-back submit");
});

test("Try again clears the old reason", async () => {
  const w = world({ reply: () => ({ ok: false, code: "not_found", error: "not found" }) });
  await w.cards.run(N("a"));
  assert.deepEqual([w.card("a1").state, w.card("a1").code], ["failed", "not_found"]);
  w.setAnswer(() => ({ ok: true }));
  await w.cards.run(N("a"));
  assert.deepEqual([w.card("a1").state, w.card("a1").code], ["done", undefined]);
});

test("no answer, a call that threw, or an unknown outcome is terminal: no Run, no Try again", async () => {
  for (const reply of [() => undefined, () => { throw new Error("Extension context invalidated"); }, () => ({ ok: false, code: "unknown_outcome", error: "the page changed while the step ran; check the page" })]) {
    const w = world({ reply });
    await w.cards.run(N("a"));
    const c = w.card("a1");
    assert.deepEqual([c.state, c.feedback, c.code], ["unknown", "Unknown outcome \u2014 check the page", undefined]);
    assert.deepEqual(P.view(c).actions, ["coder", "dismiss"]);
    await w.cards.run(N("a"));
    assert.equal(w.calls.length, 1, "pressing Run again does nothing");
    w.setAnswer(() => ({ ok: true, item: {} }));
    await w.cards.coder(N("a"));
    assert.equal(w.card("a1").state, "queued", "it can still be handed to the coder");
  }
});

test("a success says what was actually matched, so a partial match is not passed off as the card's words", async () => {
  const w = world({ reply: () => ({ ok: true, method: "script", matched: "Annual report final draft", exact: false }) });
  await w.cards.run(N("a"));
  assert.equal(w.card("a1").feedback, "Done: double-clicked \u201cAnnual report final draft\u201d");
  await w.cards.run(N("b"));
  assert.equal(w.card("a2").feedback, "Done: typed into \u201cAnnual report final draft\u201d");
  w.setAnswer(() => ({ ok: true, matched: "x\u202e\n" + "y".repeat(500) }));
  const again = world({ reply: () => ({ ok: true, matched: "Pay\u202e now\n" + "y".repeat(500) }) });
  await again.cards.run(N("a"));
  assert.ok(![...again.card("a1").feedback].some((c) => c === "\u202e" || c === "\n"));
  assert.ok(again.card("a1").feedback.length < 260);
});

test("a step done in an iframe says so, with the frame's origin, cleaned", async () => {
  const w = world({ reply: () => ({ ok: true, method: "script", matched: "GBP 200", frame: 7, frameOrigin: "https://quiz.example\u202e" }) });
  await w.cards.run(N("a"));
  assert.equal(w.card("a1").feedback, "Done: double-clicked \u201cGBP 200\u201d in an iframe (https://quiz.example)");
  const none = world({ reply: () => ({ ok: true, method: "script", frame: 3 }) });
  await none.cards.run(N("a"));
  assert.equal(none.card("a1").feedback, "Done in an iframe.");
});

test("a coder card with no page of its own refuses to queue instead of using the tab on screen", async () => {
  const w = world();
  w.card("c1").page = undefined;
  await w.cards.coder(N("d"));
  assert.deepEqual(w.calls, []);
  assert.equal(w.card("c1").state, "pending");
  assert.match(w.card("c1").feedback, /not suggested for a page/);
});

test("a coder card queues a coder chat for this tab with the goal and why, and nothing else", async () => {
  const w = world({ reply: () => ({ ok: true, item: { id: "q1" } }) });
  assert.deepEqual(w.calls, []);
  await w.cards.coder(N("d"));
  assert.deepEqual(w.calls, [{ type: "coder_queue", kind: "ask", tabId: 5, note: "Rename the files\n\nNeeds the shell", profile: "p-work" }]);
  const sent = JSON.stringify(w.calls);
  for (const forbidden of ["context", "pageText", "title", "selector", "cwd", "reload"]) assert.ok(!sent.includes(forbidden), forbidden);
  assert.deepEqual([w.card("c1").state, w.card("c1").feedback], ["queued", "Queued — press Start in the MonoAgent app."]);
  assert.deepEqual(P.view(w.card("c1")).actions, []);
  await w.cards.coder(N("d"));
  assert.equal(w.calls.length, 1, "queued once");
});

test("the request goes through coder_bridge.js's own contract, so a page's text cannot ride it", async () => {
  const sandbox = loadExtensionScripts(["coder_bridge.js"], { chrome: { runtime: { id: "x", getURL: () => "x/", onMessage: { addListener() {} } } } });
  const w = world({ reply: () => ({ ok: true, item: {} }) });
  await w.cards.coder(N("d"));
  const params = sandbox.MonoCoderBridge.queueParams(Object.assign({ pageText: "SECRET PAGE TEXT", context: "ctx", title: "t" }, w.calls[0]));
  assert.deepEqual(params, { kind: "ask", tabId: 5, note: "Rename the files\n\nNeeds the shell", profile: "p-work" });
});

test("a failed handoff leaves the coder card to press again, and a failed action card failed", async () => {
  const w = world({ reply: () => ({ ok: false, error: "MonoAgent is not running" }) });
  await w.cards.coder(N("d"));
  assert.deepEqual([w.card("c1").state, w.card("c1").feedback], ["pending", "Not queued: MonoAgent is not running"]);
  w.setAnswer(() => ({ ok: false, code: "unavailable", error: "no" }));
  await w.cards.run(N("a"));
  w.setAnswer(() => ({ ok: false, error: "MonoAgent is not running" }));
  await w.cards.coder(N("a"));
  assert.deepEqual([w.card("a1").state, w.card("a1").code, w.card("a1").feedback], ["failed", "unavailable", "Not queued: MonoAgent is not running"]);
});

test("Hand to coder from a failed action card carries the action in plain words", async () => {
  const w = world({ reply: () => ({ ok: false, code: "unavailable", error: "no" }) });
  await w.cards.run(N("b")); // a2: type "annual report" into Search
  w.setAnswer(() => ({ ok: true, item: {} }));
  await w.cards.coder(N("b"));
  const q = w.calls.at(-1);
  assert.deepEqual([q.type, q.kind, q.tabId, q.note], ["coder_queue", "ask", 5, "Type into “Search” on this page (replaces what is in the field).\n\nannual report"]);
  assert.equal(w.card("a2").state, "queued");
});

test("a handoff is not made from a page the panel left, or with no page", async () => {
  const w = world();
  w.setPage({ tabId: 5, url: "https://evil.example/" });
  await w.cards.coder(N("d"));
  assert.deepEqual(w.calls, []);
  assert.match(w.card("c1").feedback, /press Hand to coder again/);
  w.setPage(null);
  await w.cards.coder(N("d"));
  assert.deepEqual(w.calls, []);
  assert.equal(w.card("c1").feedback, "There is no page to hand over.");
});

test("dismiss ends a pending or failed action card and a pending coder card", async () => {
  const w = world({ reply: () => ({ ok: false, code: "not_found", error: "not found" }) });
  await w.cards.run(N("a"));
  await w.cards.dismiss(N("a"));
  await w.cards.dismiss(N("d"));
  assert.deepEqual([w.card("a1").state, w.card("c1").state], ["dismissed", "dismissed"]);
  assert.deepEqual(w.calls.length, 1);
});

test("an answer arriving after the panel switched profile settles nothing", async () => {
  const w = world({ reply: () => (w.switchProfile(), { ok: true }) });
  await w.cards.run(N("a"));
  assert.equal(w.card("a1").state, "sending");
});

test("a step that ran tells the panel to share the page again; a failed step, or a new tab, does not", async () => {
  let n = 0;
  const w = world({ acted: () => n++ });
  await w.cards.run(N("a"));
  assert.equal(n, 1);
  w.setAnswer(() => ({ ok: false, code: "not_found", error: "not found" }));
  await w.cards.run(N("b"));
  assert.equal(n, 1, "nothing changed on the page");
  w.setAnswer(() => ({ ok: true, method: "tab" }));
  await w.cards.run(N("c"));
  assert.equal(n, 1, "a new tab is not this page");
});

test("a field in another origin's frame shows that origin first; only a second Run confirms it", async () => {
  let n = 0;
  const w = world({ reply: (m) => (n++ === 0 ? { ok: false, code: "frame_confirm", frameOrigin: "https://ads.example", error: "x" } : { ok: true, frame: 7, frameOrigin: "https://ads.example" }) });
  await w.cards.run(N("a"));
  const c = w.card("a1");
  assert.deepEqual([c.state, c.code], ["failed", "frame_confirm"]);
  assert.match(c.feedback, /in an iframe \(https:\/\/ads\.example\)/);
  assert.match(c.feedback, /Run again/);
  assert.equal("frameOrigin" in w.calls[0], false, "the first Run confirms nothing");
  await w.cards.run(N("a"));
  assert.equal(w.calls[1].frameOrigin, "https://ads.example");
  assert.equal(w.card("a1").state, "done");
});
