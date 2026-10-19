// What a click on a side panel chat card does (sidepanel_chat_cards.js,
// issue #451): a task card's Add goes to the worker; a recording card's
// Start recording starts the Record section's recorder, the card follows
// that recording, and its Review and save runs the analysis. Nothing here can
// save an automation: that is the Record section's own Save.
// `CHROME_PATH=/nonexistent node --test chrome-extension/sidepanel_chat_cards.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const { MonoChatCards: C } = loadExtensionScripts(["chat_proposals.js", "sidepanel_chat_cards.js"]);

// A card is named by its key: its nonce, or its call id when it has none (r1).
const T1 = "0123456789abcdef";

function world({ start, analyze, taskReply } = {}) {
  const chat = {
    messages: [{
      id: "m1", role: "assistant", text: "", tools: [],
      proposals: [
        { id: "r1", kind: "recording", goal: "Export the report", steps: "Open Reports.", untrustedPage: true, state: "pending", feedback: "" },
        { id: "t1", kind: "task", title: "Book flights", notes: "", untrustedPage: false, state: "pending", feedback: "", nonce: "0123456789abcdef" },
      ],
    }],
  };
  const calls = [];
  let saves = 0;
  let seq = 1;
  let page = { tabId: 5, url: "https://a.example/p" };
  const record = {
    start: async (goal, tabId) => (calls.push(tabId !== undefined ? ["start", goal, tabId] : ["start", goal]), start ? start(goal) : { recording: true, id: "rec-1" }),
    analyze: async (id) => (calls.push(["analyze", id]), analyze ? analyze(id) : { draftDir: "/d" }),
  };
  let reply = () => taskReply || { ok: true, status: "added", feedback: { text: "Added to Inbox in Work (#4)" } };
  const cards = C.create({
    chat: () => chat,
    ask: async (m) => (calls.push(["ask", m]), reply()),
    profile: () => "p-work",
    current: () => seq,
    save: async () => saves++,
    draw: () => {},
    record,
    page: () => page,
  });
  const card = (id) => chat.messages[0].proposals.find((p) => p.id === id);
  const state = (s) => cards.follow({ type: "state", state: Object.assign({ id: "rec-1", recording: false, steps: [{}, {}, {}] }, s) });
  return { chat, calls, cards, card, state, record, saves: () => saves, switchProfile: () => seq++, setReply: (fn) => (reply = fn), setPage: (p) => (page = p) };
}

test("a recording card starts nothing until Start recording, then starts the recorder with its goal, once", async () => {
  const w = world();
  assert.deepEqual(w.calls, [], "nothing happens before a click");
  await w.cards.confirm("r1");
  assert.deepEqual(w.calls, [["start", "Export the report"]]);
  assert.deepEqual([w.card("r1").state, w.card("r1").recordingId], ["recording", "rec-1"]);
  assert.ok(w.saves() > 0, "kept, so a reopened panel still follows it");
  await w.cards.confirm("r1");
  assert.equal(w.calls.length, 1, "a second click starts nothing more");
});

test("a recording that cannot start says why, and can be tried again", async () => {
  let fail = true;
  const w = world({ start: () => { if (fail) throw new Error("a recording is already running"); return { recording: true, id: "rec-2" }; } });
  await w.cards.confirm("r1");
  assert.equal(w.card("r1").state, "refused");
  assert.match(w.card("r1").feedback, /already running/);
  fail = false;
  await w.cards.confirm("r1");
  assert.deepEqual([w.card("r1").state, w.card("r1").recordingId], ["recording", "rec-2"]);
});

test("the card follows its own recording: delivered steps offer Review and save", async () => {
  const w = world();
  await w.cards.confirm("r1");
  w.state({ recording: true });
  assert.equal(w.card("r1").state, "recording");
  w.state({ delivered: false });
  assert.equal(w.card("r1").state, "recording");
  assert.match(w.card("r1").feedback, /Waiting for MonoAgent/);
  w.state({ delivered: true });
  assert.deepEqual([w.card("r1").state, w.card("r1").feedback], ["recorded", "Recorded 3 steps."]);
  w.state({ id: "rec-9", recording: true, steps: [] });
  assert.equal(w.card("r1").state, "recorded", "another recording does not move a recorded card");
  w.cards.follow({ type: "state", state: { id: "", recording: false, steps: [] } });
  assert.equal(w.card("r1").state, "recorded", "an empty Record section says nothing about it");
});

test("a recording with no steps, or one the Record section has moved on from, offers to record again", async () => {
  const w = world();
  await w.cards.confirm("r1");
  w.state({ discarded: true, steps: [] });
  assert.deepEqual([w.card("r1").state, w.card("r1").feedback], ["empty", "Nothing was recorded."]);

  const v = world();
  await v.cards.confirm("r1");
  v.state({ id: "rec-other" });
  assert.equal(v.card("r1").state, "empty");
  assert.match(v.card("r1").feedback, /no longer in the Record section/);
});

test("Review and save analyzes that recording; the Record section's Save settles the card", async () => {
  const w = world();
  await w.cards.confirm("r1");
  w.state({ delivered: true });
  await w.cards.review("r1");
  assert.deepEqual(w.calls.at(-1), ["analyze", "rec-1"]);
  assert.equal(w.card("r1").state, "reviewing");
  assert.match(w.card("r1").feedback, /press Save there/);
  w.cards.follow({ type: "saved", recordingId: "rec-9", text: "Saved something else" });
  assert.equal(w.card("r1").state, "reviewing", "another recording's save is not this card's");
  w.cards.follow({ type: "saved", recordingId: "rec-1", text: "Saved the action export in automation reports" });
  assert.deepEqual([w.card("r1").state, w.card("r1").feedback], ["saved", "Saved the action export in automation reports"]);
  assert.equal(w.calls.filter((c) => c[0] === "ask").length, 0, "the card itself saves nothing");
});

test("a save made in the Record section without the card's Review still settles it", async () => {
  const w = world();
  await w.cards.confirm("r1");
  w.state({ delivered: true });
  w.cards.follow({ type: "saved", recordingId: "rec-1", text: "Saved." });
  assert.equal(w.card("r1").state, "saved");
});

test("a failed analysis says so and offers Review and save again", async () => {
  const w = world({ analyze: () => { throw new Error("the bridge is not running"); } });
  await w.cards.confirm("r1");
  w.state({ delivered: true });
  await w.cards.review("r1");
  assert.equal(w.card("r1").state, "recorded");
  assert.match(w.card("r1").feedback, /Analysis failed: the bridge is not running/);
  await w.cards.review(T1);
  assert.equal(w.calls.filter((c) => c[0] === "analyze").length, 1, "Review is a recording card's only");
});

test("a task card's Add still goes to the worker once, and Dismiss sends nothing", async () => {
  const w = world();
  await w.cards.confirm(T1);
  await w.cards.confirm(T1);
  const asked = w.calls.filter((c) => c[0] === "ask").map((c) => c[1]);
  assert.deepEqual(asked, [{ type: "task_add_proposal", profile: "p-work", proposal: { id: "t1", kind: "task", title: "Book flights", notes: "", nonce: "0123456789abcdef" } }]);
  assert.deepEqual([w.card("t1").state, w.card("t1").feedback], ["added", "Added to Inbox in Work (#4)"]);
  await w.cards.dismiss("r1");
  assert.equal(w.card("r1").state, "dismissed");
  assert.equal(w.calls.filter((c) => c[0] === "start").length, 0);
});

test("a start answered after the panel moved to another profile leaves the card alone", async () => {
  let release;
  const w = world({ start: () => new Promise((r) => (release = r)) });
  const pressed = w.cards.confirm("r1");
  await new Promise((r) => setTimeout(r, 1));
  w.switchProfile();
  release({ recording: true, id: "rec-1" });
  await pressed;
  assert.equal(w.card("r1").recordingId, undefined);
});

// The lead's two rules for the handoff: Start recording is the only way a
// card starts a recording, and Review and save cannot save.
test("only Start recording starts one: following, reviewing, dismissing and saves elsewhere start nothing", async () => {
  const w = world();
  // A recording someone started from the Record section, and all that follows it.
  w.state({ id: "rec-5", recording: true });
  w.state({ id: "rec-5", recording: false, delivered: true });
  w.cards.follow({ type: "saved", recordingId: "rec-5", text: "Saved." });
  await w.cards.review("r1");
  assert.deepEqual([w.card("r1").state, w.card("r1").recordingId], ["pending", undefined], "a card never adopts a recording it did not start");
  await w.cards.dismiss("r1");
  await w.cards.confirm("r1");
  assert.equal(w.calls.filter((c) => c[0] === "start").length, 0, "nothing started, and a dismissed card starts nothing");
});

test("Review and save cannot save: it analyzes, and the card is saved only when the Record section says so", async () => {
  const w = world();
  const saves = [];
  w.record.save = () => saves.push("save");
  await w.cards.confirm("r1");
  w.state({ delivered: true });
  await w.cards.review("r1");
  await w.cards.review("r1");
  w.state({ delivered: true });
  assert.equal(w.card("r1").state, "reviewing");
  assert.deepEqual(saves, []);
  assert.deepEqual(w.calls.filter((c) => c[0] === "ask"), [], "nothing reaches the worker");
  assert.deepEqual(w.calls.map((c) => c[0]), ["start", "analyze"], "one analysis for two clicks while it is open");
});

// Two turns' recording cards can share a call id (a runtime that numbers its
// calls per turn): Start recording starts its own card's goal, not the other's.
test("a click starts its own card when two recording cards share a call id", async () => {
  const w = world();
  const proposals = w.chat.messages[0].proposals;
  proposals[0] = { id: "toolu_rec1", kind: "recording", goal: "Old goal", steps: "", untrustedPage: false, state: "saved", feedback: "Saved.", nonce: "1111111111111111", recordingId: "rec-0" };
  proposals[1] = { id: "toolu_rec1", kind: "recording", goal: "New goal", steps: "", untrustedPage: false, state: "pending", feedback: "", nonce: "2222222222222222" };
  await w.cards.confirm("2222222222222222");
  assert.deepEqual(w.calls, [["start", "New goal"]]);
  assert.deepEqual(proposals.map((p) => p.state), ["saved", "recording"]);
});

// #451: a vault card's Save asks the worker to fetch and save its item, from
// the panel's page; nothing goes before the click, and once per click.
test("a vault card's Save sends its item with the panel's page, once, and says what became of it", async () => {
  const replies = [{ ok: false, status: "retry", feedback: { text: "Not saved: that file is not on the page you are on" } }, { ok: true, status: "saved", created: true, feedback: { text: "Saved to the vault in Work: q3.pdf" } }];
  const w = world({ taskReply: null });
  const proposals = w.chat.messages[0].proposals;
  proposals.push({ id: "toolu_v", kind: "vault", url: "https://a.example/q3.pdf", selection: false, name: "", untrustedPage: true, state: "pending", feedback: "", nonce: "3333333333333333" });
  w.setReply(() => replies.shift());
  const first = w.cards.confirm("3333333333333333");
  await w.cards.confirm("3333333333333333");
  await first;
  const asked = w.calls.filter((c) => c[0] === "ask").map((c) => c[1]);
  assert.deepEqual(asked, [{ type: "vault_save_proposal", profile: "p-work", tab: { tabId: 5, url: "https://a.example/p" },
    proposal: { id: "toolu_v", kind: "vault", url: "https://a.example/q3.pdf", selection: false, name: "", nonce: "3333333333333333" } }]);
  assert.deepEqual([proposals[2].state, proposals[2].feedback], ["pending", "Not saved: that file is not on the page you are on"], "a retry keeps the card");
  await w.cards.confirm("3333333333333333");
  assert.deepEqual([proposals[2].state, proposals[2].feedback], ["saved", "Saved to the vault in Work: q3.pdf"]);
  assert.equal(w.calls.filter((c) => c[0] === "start").length, 0);
});

// #876 review: Start records the page the card was proposed on, with the goal
// the person has seen and could change; another page in the panel starts nothing.
test("Start records the card's own page with the goal from its field, and refuses another page", async () => {
  const w = world();
  const card = w.card("r1");
  card.page = { tabId: 5, url: "https://a.example/p" };
  w.setPage({ tabId: 6, url: "https://other.example/x" });
  await w.cards.confirm("r1", "Export the weekly report");
  assert.equal(w.calls.filter((c) => c[0] === "start").length, 0);
  assert.equal(card.state, "pending", "it can be pressed again from that page");
  assert.match(card.feedback, /a\.example\/p/);
  assert.match(card.feedback, /other\.example\/x/);
  w.setPage({ tabId: 5, url: "https://a.example/p#section" });
  await w.cards.confirm("r1", " Export the <b>weekly</b> report\u0007 ");
  assert.deepEqual(w.calls.filter((c) => c[0] === "start"), [["start", "Export the weekly report", 5]]);
  assert.equal(card.goal, "Export the weekly report", "the card shows what was recorded");
});

test("a goal emptied in its field starts nothing and says why", async () => {
  const w = world();
  await w.cards.confirm("r1", " <b></b> ");
  assert.equal(w.calls.filter((c) => c[0] === "start").length, 0);
  assert.match(w.card("r1").feedback, /what you will do/i);
});
