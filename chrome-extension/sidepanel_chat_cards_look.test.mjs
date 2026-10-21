// The look card (issue #941): propose_page_look draws a card; Run takes the
// picture; the first one in a chat is shown and waits for "Send to the model";
// the picture is never stored with the card; a private page cannot be looked at.
// `CHROME_PATH=/nonexistent node --test chrome-extension/sidepanel_chat_cards_look.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const { MonoChatCards: C, MonoChatProposals: P } = loadExtensionScripts(["private_net.js", "agent_redact.js", "chat_proposals_act.js", "chat_proposals.js", "sidepanel_chat_cards.js"]);

const PAGE = { tabId: 5, url: "https://app.example/dash?token=hunter2" };
const N = (c) => c.repeat(16);
const IMAGE = { mime: "image/jpeg", data: "/9j/SECRETPIXELS", width: 1280, height: 640, bytes: 51200 };

function world({ capture, sendLook, consent = false, page = PAGE, cardPage = PAGE } = {}) {
  const chat = {
    lookOrigins: consent ? [new URL(cardPage.url).origin] : [],
    messages: [{
      id: "m1", role: "assistant", text: "", tools: [],
      proposals: [
        { id: "l1", kind: "look", why: "The chart is on a canvas", untrustedPage: true, state: "pending", feedback: "", nonce: N("a"), page: cardPage },
        { id: "l2", kind: "look", why: "", untrustedPage: true, state: "pending", feedback: "", nonce: N("b"), page: cardPage },
      ],
    }],
  };
  const calls = [];
  const sends = [];
  let where = page;
  const cards = C.create({
    chat: () => chat,
    ask: async (m) => (calls.push(m), capture ? capture(m) : { ok: true, image: IMAGE, url: "https://app.example/dash?token=•••" }),
    sendLook: async (image) => (sends.push(image), sendLook ? sendLook(image) : { sent: true }),
    profile: () => "p-work",
    current: () => 1,
    save: async () => {},
    draw: () => {},
    record: null,
    page: () => where,
  });
  const card = (id) => chat.messages[0].proposals.find((p) => p.id === id);
  return { chat, calls, sends, cards, card, move: (p) => (where = p) };
}

test("nothing is captured until Run; the first picture is shown and waits for Send", async () => {
  const w = world();
  assert.deepEqual(w.calls, []);
  await w.cards.run(N("a"));
  assert.deepEqual(w.calls, [{ type: "page_look_capture", tab: PAGE }]);
  assert.equal(w.card("l1").state, "preview");
  assert.deepEqual(w.sends, [], "nothing has left the browser");
  assert.match(w.cards.thumb(N("a")), /^data:image\/jpeg;base64,/);
  assert.equal(P.view(w.card("l1")).image, true);

  await w.cards.send(N("a"));
  assert.deepEqual(w.sends, [IMAGE]);
  assert.equal(w.card("l1").state, "done");
  assert.match(w.card("l1").feedback, /1280.640, 50 KB/);
  assert.equal(w.cards.thumb(N("a")), "", "the picture is dropped once sent");
});

test("once the person has agreed in a chat, Run sends straight away; a new chat asks again", async () => {
  const w = world();
  await w.cards.run(N("a"));
  await w.cards.send(N("a"));
  assert.deepEqual(w.chat.lookOrigins, ["https://app.example"]);
  await w.cards.run(N("b"));
  assert.equal(w.card("l2").state, "done");
  assert.equal(w.sends.length, 2);
  const fresh = world();
  await fresh.cards.run(N("a"));
  assert.equal(fresh.card("l1").state, "preview", "consent belongs to the chat");
});

test("agreeing for one site does not agree for another: a card for a new origin shows its picture first", async () => {
  const BANK = { tabId: 6, url: "https://bank.example/account" };
  const w = world({ consent: true });
  w.chat.messages[0].proposals.push({ id: "l3", kind: "look", why: "", untrustedPage: true, state: "pending", feedback: "", nonce: N("c"), page: BANK });
  w.move(BANK);
  await w.cards.run(N("c"));
  assert.equal(w.card("l3").state, "preview", "waits for Send to the model");
  assert.deepEqual(w.sends, [], "nothing left the browser");
  await w.cards.send(N("c"));
  assert.equal(w.sends.length, 1);
  assert.deepEqual(w.chat.lookOrigins.sort(), ["https://app.example", "https://bank.example"]);
  // Back on the first site, that agreement still holds.
  w.move(PAGE);
  await w.cards.run(N("a"));
  assert.equal(w.card("l1").state, "done");
});

test("the picture is never in the stored card or the chat state", async () => {
  const w = world();
  await w.cards.run(N("a"));
  assert.ok(!JSON.stringify(w.chat).includes("SECRETPIXELS"));
  assert.ok(!JSON.stringify(P.clone(w.chat.messages[0].proposals)).includes("SECRETPIXELS"));
  assert.ok(!JSON.stringify(P.view(w.card("l1"))).includes("SECRETPIXELS"));
});

test("Dismiss at the preview drops the picture and sends nothing", async () => {
  const w = await (async () => {
    const x = world();
    await x.cards.run(N("a"));
    return x;
  })();
  await w.cards.dismiss(N("a"));
  assert.equal(w.card("l1").state, "dismissed");
  assert.equal(w.cards.thumb(N("a")), "");
  await w.cards.send(N("a"));
  assert.deepEqual(w.sends, []);
  assert.equal(w.card("l1").state, "dismissed");
});

test("a refused or failed capture is said, and Run can be tried again; nothing is sent", async () => {
  const w = world({ capture: () => ({ ok: false, code: "not_allowed", error: "192.168.0.1 is on a private network" }) });
  await w.cards.run(N("a"));
  assert.equal(w.card("l1").state, "failed");
  assert.match(w.card("l1").feedback, /Not taken: 192.168.0.1 is on a private network/);
  assert.deepEqual(w.sends, []);
  assert.deepEqual(P.view(w.card("l1")).actions, ["run", "dismiss"]);
  const gone = world({ capture: () => null });
  await gone.cards.run(N("a"));
  assert.match(gone.card("l1").feedback, /no answer/);
});

test("a send that fails says why and keeps nothing; trying again takes a new picture", async () => {
  const w = world({ consent: true, sendLook: () => ({ sent: false, error: "this monomind cannot show images to the model" }) });
  await w.cards.run(N("a"));
  assert.equal(w.card("l1").state, "failed");
  assert.match(w.card("l1").feedback, /Not sent: this monomind cannot show images/);
  assert.equal(w.cards.thumb(N("a")), "");
  assert.deepEqual(w.chat.lookOrigins, ["https://app.example"]);
});

test("a card made for another page does not capture this one", async () => {
  const w = world({ page: { tabId: 5, url: "https://other.example/x" } });
  await w.cards.run(N("a"));
  assert.deepEqual(w.calls, []);
  assert.match(w.card("l1").feedback, /suggested for/);
  assert.equal(w.card("l1").state, "pending");
  const noPage = world({ cardPage: null });
  await noPage.cards.run(N("a"));
  assert.deepEqual(noPage.calls, []);
});

test("a card on a private-network page offers no Run at all", () => {
  const priv = { id: "x", kind: "look", why: "", untrustedPage: false, state: "pending", feedback: "", nonce: N("c"), page: { tabId: 1, url: "http://192.168.1.1/admin" } };
  const v = P.view(priv);
  assert.deepEqual(v.actions, ["dismiss"]);
  assert.match(v.source, /private network/);
});

test("the address on the card has its secrets masked", () => {
  const v = P.view({ id: "x", kind: "look", why: "", untrustedPage: false, state: "pending", feedback: "", nonce: N("c"), page: PAGE });
  assert.ok(!JSON.stringify(v).includes("hunter2"), JSON.stringify(v));
  assert.match(v.source, /app\.example\/dash/);
});

test("propose_page_look is a card; a stored one cut off at the preview starts over, one cut off mid-send is not sent again", () => {
  const card = P.fromTool("propose_page_look", { result: JSON.stringify({ proposal: { kind: "look", why: " the chart " }, added: false }) }, "call-1", true);
  assert.equal(card.kind, "look");
  assert.equal(card.why, "the chart");
  assert.equal(card.state, "pending");
  assert.equal(P.fromTool("propose_page_look", { result: JSON.stringify({ proposal: { kind: "task", title: "x" } }) }, "c", true), null);
  const stored = (state) => P.clone([{ ...card, state }])[0];
  assert.equal(stored("preview").state, "pending");
  assert.equal(stored("capturing").state, "pending");
  assert.equal(stored("sending").state, "failed");
  assert.deepEqual(P.view(stored("sending")).actions, ["run", "dismiss"]);
  assert.match(stored("sending").feedback, /may or may not have been sent/);
});
