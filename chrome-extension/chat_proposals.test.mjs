// The side panel chat's proposals (chat_proposals.js, issue #451): a proposal
// tool's answer becomes a card, checked again here; nothing is sent but by the
// person's click; a card made after the conversation read a page says so.
// `CHROME_PATH=/nonexistent node --test chrome-extension/chat_proposals.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const { MonoChatProposals: P } = loadExtensionScripts(["chat_proposals.js"]);

const answer = (proposal) => ({ result: JSON.stringify({ proposal, added: false, note: "…" }) });

test("a propose_task answer is a pending task card; anything else is not a card", () => {
  const { nonce, ...p } = P.fromTool("propose_task", answer({ kind: "task", title: "Book flights", notes: "Before May" }), "toolu_01", false);
  assert.match(nonce, /^[0-9a-f]{16}$/, "each card gets its own key");
  assert.deepEqual(p, { id: "toolu_01", kind: "task", title: "Book flights", notes: "Before May", untrustedPage: false, state: "pending", feedback: "" });
  assert.equal(P.fromTool("list_workflows", answer({ kind: "task", title: "x" }), "t", false), null, "not a proposal tool");
  assert.equal(P.fromTool("propose_task", { result: "not json" }, "t", false), null);
  assert.equal(P.fromTool("propose_task", answer({ kind: "vault_save", title: "x" }), "t", false), null, "the kind must be the tool's");
  assert.equal(P.fromTool("propose_task", answer({ kind: "task", title: "   " }), "t", false), null, "no title, no card");
  assert.equal(P.fromTool("propose_task", answer({ kind: "task", title: "x" }), "", false), null, "no call id, no card");
});

test("a card's words are checked again: one line of title, capped notes", () => {
  const p = P.fromTool("propose_task", answer({ kind: "task", title: "Book\nthe   flights" + "é".repeat(400), notes: "n".repeat(9000) }), "t", true);
  assert.ok(!p.title.includes("\n"));
  assert.ok(p.title.startsWith("Book the flights"));
  assert.equal([...p.title].length, 200);
  assert.ok(new TextEncoder().encode(p.notes).length <= 4000);
  assert.equal(p.untrustedPage, true);
});

test("a stored card is checked again on load, and keeps what became of it", () => {
  const stored = [
    { id: "a", kind: "task", title: "T", notes: "", untrustedPage: true, state: "added", feedback: "Added to Inbox in Work (#12)" },
    { id: "b", kind: "task", title: "", state: "pending" },
    { id: "c", kind: "nope", title: "x" },
    { id: "d", kind: "task", title: "U", state: "sending" },
  ];
  const out = P.clone(stored);
  assert.deepEqual(out.map((p) => [p.id, p.state]), [["a", "added"], ["d", "pending"]], "a send cut off by a closed panel can be pressed again");
  assert.equal(out[0].feedback, "Added to Inbox in Work (#12)");
});

// #868: the board dedupes on (profile, client_id) across every task, so a
// key made from the call id alone could collide (a reused id, or two that
// sanitize alike) and hand back an older task. Each card has its own nonce.
test("cards whose call ids sanitize alike still get their own task id, stable for each card", () => {
  const card = (id) => P.fromTool("propose_task", answer({ kind: "task", title: "T" }), id, false);
  const a = card("functions.x:0");
  const b = card("functionsx0");
  const c = card("!!!");
  const d = card("???");
  const ids = [a, b, c, d].map((p) => P.clientId(p));
  assert.equal(new Set(ids).size, 4, ids.join(" "));
  for (const id of ids) assert.match(id, /^[A-Za-z0-9_-]{1,64}$/);
  assert.equal(P.clientId(P.clone([a])[0]), P.clientId(a), "a stored card keeps its key, so a resend adds nothing twice");
  assert.equal(P.clientId({ id: "x", nonce: "../not-a-nonce" }), "chat-x", "an unusable nonce is not used");
  assert.equal(P.clone([{ id: "x", kind: "task", title: "T", nonce: "<b>" }])[0].nonce, undefined);
  assert.ok(P.clientId({ id: "y".repeat(200), nonce: a.nonce }).length <= 64);
  const forged = P.fromTool("propose_task", answer({ kind: "task", title: "T", nonce: "0123456789abcdef" }), "t", false);
  assert.notEqual(forged.nonce, "0123456789abcdef", "an answer cannot choose the key");
});

test("a proposal's task id is stable, so a second click or a resend adds nothing twice", () => {
  assert.equal(P.clientId({ id: "toolu_01AbC" }), "chat-toolu_01AbC");
  assert.equal(P.clientId({ id: "call/../x y" }), "chat-callxy");
  assert.ok(P.clientId({ id: "x".repeat(200) }).length <= 64);
});

test("the card says what it is, flags a page that was read, and offers Add only while pending", () => {
  const pending = P.view({ id: "a", kind: "task", title: "T", notes: "N", untrustedPage: true, state: "pending" });
  assert.equal(pending.heading, "Add a task?");
  assert.match(pending.warning, /web page/);
  assert.deepEqual(pending.actions, ["add", "dismiss"]);
  const added = P.view({ id: "a", kind: "task", title: "T", untrustedPage: false, state: "added", feedback: "Added to Inbox in Work (#12)" });
  assert.equal(added.warning, "");
  assert.deepEqual(added.actions, []);
  assert.equal(added.status, "Added to Inbox in Work (#12)");
  assert.equal(P.view({ id: "a", kind: "task", title: "T", state: "dismissed" }).status, "Not added");
});

// A tiny DOM: just what the card builder calls, and no innerHTML at all.
class Node_ {
  constructor(tag) { this.tag = tag; this.children = []; this.dataset = {}; this.listeners = {}; this._text = ""; this.className = ""; this.type = ""; this.disabled = false; this.attrs = {}; }
  appendChild(c) { this.children.push(c); return c; }
  append(...cs) { for (const c of cs) this.appendChild(c); }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  addEventListener(t, fn) { (this.listeners[t] = this.listeners[t] || []).push(fn); }
  set textContent(v) { this._text = String(v); this.children = []; }
  get textContent() { return this._text + this.children.map((c) => c.textContent).join(""); }
  set innerHTML(v) { throw new Error("innerHTML was used"); }
  all() { return [this, ...this.children.flatMap((c) => c.all())]; }
}
const doc = { createElement: (t) => new Node_(t), createTextNode: (t) => Object.assign(new Node_("#text"), { _text: t }) };

test("the card is built from text nodes, and its buttons call back only when pressed", () => {
  const calls = [];
  const card = P.card(doc, { id: "a", kind: "task", title: "<img src=x onerror=alert(1)>", notes: "**bold**", untrustedPage: true, state: "pending" }, {
    add: () => calls.push("add"),
    dismiss: () => calls.push("dismiss"),
  });
  assert.equal(card.dataset.state, "pending");
  assert.equal(card.dataset.untrusted, "true");
  assert.match(card.textContent, /<img src=x onerror=alert\(1\)>/, "shown as text");
  assert.ok(!card.all().some((n) => n.tag === "img"));
  const buttons = card.all().filter((n) => n.tag === "button");
  assert.deepEqual(buttons.map((b) => b.textContent), ["Add to Inbox", "Dismiss"]);
  assert.deepEqual(calls, [], "nothing happens until a button is pressed");
  buttons[0].listeners.click[0]({});
  assert.deepEqual(calls, ["add"]);
});

test("settle finds a card by its key, so cards that share a call id are told apart", () => {
  const chat = { messages: [
    { proposals: [{ id: "functions.propose_task:0", kind: "task", title: "A", state: "pending", nonce: "1111111111111111" }] },
    { proposals: [{ id: "functions.propose_task:0", kind: "task", title: "B", state: "pending", nonce: "2222222222222222" }] },
  ] };
  assert.equal(P.keyOf(chat.messages[1].proposals[0]), "2222222222222222");
  assert.equal(P.keyOf({ id: "legacy" }), "legacy", "a card without a nonce is known by its call id");
  assert.ok(P.settle(chat, "2222222222222222", { state: "added" }));
  assert.deepEqual(chat.messages.map((m) => m.proposals[0].state), ["pending", "added"]);
});
