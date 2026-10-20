// The chat's two "do something" proposal kinds (chat_proposals_act.js, folded
// into chat_proposals.js): propose_page_action -> an action card and
// propose_coder_handoff -> a coder card. The answer is a model's, steered by
// whatever page it read, so it is checked again: actions known, addresses
// http(s), everything clamped and stripped of invisible characters, and the
// card is built from text nodes only.
// `CHROME_PATH=/nonexistent node --test chrome-extension/chat_proposals_act.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const { MonoChatProposals: P } = loadExtensionScripts(["private_net.js", "chat_proposals_act.js", "chat_proposals.js"]);

const answer = (proposal) => ({ result: JSON.stringify({ proposal, added: false, note: "…" }) });
const act = (extra) => P.fromTool("propose_page_action", answer(Object.assign({ kind: "action", action: "click", target: "Save draft" }, extra)), "toolu_a", false);

test("a propose_page_action answer is a pending action card; the kind must be the tool's", () => {
  const { nonce, ...p } = act({ action: "double_click", target: "CV - Morteza Nokhodian 2025 Apr" });
  assert.match(nonce, /^[0-9a-f]{16}$/);
  assert.deepEqual(p, { id: "toolu_a", kind: "action", action: "double_click", target: "CV - Morteza Nokhodian 2025 Apr", text: "", url: "", untrustedPage: false, state: "pending", feedback: "" });
  assert.equal(P.fromTool("propose_page_action", answer({ kind: "coder", goal: "g" }), "t", false), null);
  assert.equal(P.fromTool("propose_task", answer({ kind: "action", action: "click", target: "x" }), "t", false), null);
  assert.equal(P.fromTool("propose_page_action", answer({ kind: "action", action: "click", target: "x" }), "", false), null, "no call id, no card");
  assert.equal(act({}).untrustedPage, false);
  assert.equal(P.fromTool("propose_page_action", answer({ kind: "action", action: "click", target: "x" }), "t", true).untrustedPage, true, "a conversation that read a page says so");
});

test("an unknown action, or one missing what it needs, is no card", () => {
  for (const bad of [
    { action: "delete" }, { action: "" }, { action: undefined }, { action: "CLICK" },
    { target: "" }, { target: "   " }, { target: 5 },
    { action: "type", text: "" }, { action: "type", text: undefined },
    { action: "open_url", url: "" }, { action: "open_url", url: undefined },
  ]) {
    assert.equal(act(bad), null, JSON.stringify(bad));
  }
  assert.ok(act({ action: "open_url", target: "", url: "https://a.example/x" }), "open_url needs no target");
  assert.ok(act({ action: "type", target: "Search", text: "hello" }));
});

test("open_url takes http and https addresses only, short, without spaces or control characters", () => {
  for (const url of [
    "javascript:alert(1)", "data:text/html,<b>", "file:///etc/passwd", "chrome://settings", "ftp://a.example/x", "//a.example/x", "a.example/x",
    "https://a.example/a b", "https://a.example/\u0000", "https://a.example/a\nb", "https://", "https://a.example/" + "x".repeat(2100),
  ]) {
    assert.equal(act({ action: "open_url", url }), null, url.slice(0, 40));
  }
  const ok = act({ action: "open_url", url: " http://localhost:8080/x?y=1 " });
  assert.equal(ok.url, "http://localhost:8080/x?y=1");
  assert.equal(act({ action: "click", url: "javascript:x" }).url, "", "a click carries no address");
});

test("an action's words are clamped: target 200, typed text 2000, invisible characters gone", () => {
  const long = act({ target: "é".repeat(500) });
  assert.equal([...long.target].length, 200);
  const hidden = act({ target: "Save​‮ draft\u0007\n  now\u{E0041}" });
  assert.equal(hidden.target, "Save draft now");
  const marked = act({ target: "<b>Save</b> <script>x</script>" });
  assert.ok(!/[<>]/.test(marked.target));
  const typed = act({ action: "type", target: "Notes", text: "line one\r\nline\ttwo\u0000‮" + "x".repeat(3000) });
  assert.equal([...typed.text].length, 2000);
  assert.ok(typed.text.startsWith("line one\nline two"), "line breaks stay, other controls go");
  assert.ok(!/[\u0000‮\t]/.test(typed.text));
  assert.equal(act({ action: "click", text: "ignored" }).text, "", "only a type card keeps text");
  assert.equal(act({ action: "open_url", target: "ignored", url: "https://a.example/" }).target, "");
});

test("a propose_coder_handoff answer is a coder card: goal 2000, why 300, one line each", () => {
  const { nonce, ...p } = P.fromTool("propose_coder_handoff", answer({ kind: "coder", goal: "Fix\nthe   header", why: "Needs\nedits" }), "toolu_c", true);
  assert.match(nonce, /^[0-9a-f]{16}$/);
  assert.deepEqual(p, { id: "toolu_c", kind: "coder", goal: "Fix the header", why: "Needs edits", untrustedPage: true, state: "pending", feedback: "" });
  const big = P.fromTool("propose_coder_handoff", answer({ kind: "coder", goal: "g".repeat(5000), why: "w".repeat(900) + "​" }), "t", false);
  assert.deepEqual([[...big.goal].length, [...big.why].length], [2000, 300]);
  assert.equal(P.fromTool("propose_coder_handoff", answer({ kind: "coder", goal: "  " }), "t", false), null, "no goal, no card");
  assert.equal(P.fromTool("propose_coder_handoff", answer({ kind: "action", action: "click", target: "x" }), "t", false), null);
  assert.equal(P.fromTool("propose_coder_handoff", answer({ kind: "coder", goal: "g", state: "queued" }), "t", false).state, "pending", "an answer cannot settle its own card");
});

test("stored cards are checked again; a card cut off while sending is pending again, and keeps why it failed", () => {
  const out = P.clone([
    { id: "a", kind: "action", action: "click", target: "Go", state: "sending", feedback: "" },
    { id: "b", kind: "action", action: "click", target: "Go", state: "failed", code: "submit_blocked", feedback: "Not done: x" },
    { id: "c", kind: "action", action: "click", target: "Go", state: "failed", code: "<x>", feedback: "" },
    { id: "d", kind: "action", action: "wipe", target: "Go", state: "pending" },
    { id: "e", kind: "coder", goal: "G", why: "W", state: "queued", feedback: "Queued" },
    { id: "f", kind: "coder", goal: "G", state: "done" },
    { id: "g", kind: "action", action: "click", target: "Go", state: "queued" },
  ]);
  assert.deepEqual(out.map((p) => p.id + ":" + p.state), ["a:pending", "b:failed", "c:failed", "e:queued", "f:pending", "g:queued"]);
  assert.equal(out[1].code, "submit_blocked");
  assert.equal(out[2].code, undefined, "a code is a plain word or nothing");
});

test("settle records a failed action's code and clears it when the card runs again", () => {
  const card = act({});
  const chat = { messages: [{ proposals: [card] }] };
  assert.equal(P.settle(chat, P.keyOf(card), { state: "failed", feedback: "Not done", code: "ambiguous" }), true);
  assert.deepEqual([card.state, card.code], ["failed", "ambiguous"]);
  P.settle(chat, P.keyOf(card), { state: "sending", feedback: "", code: null });
  assert.equal(card.code, undefined);
  P.settle(chat, P.keyOf(card), { state: "added" });
  assert.equal(card.state, "sending", "a state the kind does not have is not taken");
});

test("an action card says it in plain words; a typed text and an address are shown, a private address is flagged", () => {
  const v = (extra) => P.view(Object.assign({ id: "a", kind: "action", state: "pending", untrustedPage: false, text: "", url: "" }, extra));
  assert.equal(v({ action: "double_click", target: "CV - Morteza Nokhodian 2025 Apr" }).title, "Double-click “CV - Morteza Nokhodian 2025 Apr” on this page");
  assert.equal(v({ action: "click", target: "Save" }).title, "Click “Save” on this page");
  const t = v({ action: "type", target: "Search", text: "hello" });
  assert.deepEqual([t.title, t.notes], ["Type into “Search” on this page", "hello"]);
  const o = v({ action: "open_url", url: "https://a.example/x" });
  assert.deepEqual([o.title, o.notes, o.source], ["Open a.example in a new tab", "https://a.example/x", undefined]);
  assert.match(v({ action: "open_url", url: "http://192.168.1.1/admin" }).source, /private network/);
  assert.match(v({ action: "click", target: "Save", untrustedPage: true }).warning, /after reading a web page.*Check it before you run it/);
  assert.deepEqual(v({ action: "click", target: "S" }).actions, ["run", "dismiss"]);
  assert.equal(v({ action: "click", target: "S", state: "sending" }).status, "Running…");
  assert.equal(v({ action: "click", target: "S", state: "done", feedback: "Done." }).status, "Done.");
  const failed = v({ action: "click", target: "S", state: "failed", feedback: "Not done: not found", code: "not_found" });
  assert.deepEqual([failed.actions, failed.labels.run, failed.labels.coder], [["run", "coder", "dismiss"], "Try again", "Hand to coder"]);
  const blocked = v({ action: "click", target: "S", state: "failed", feedback: "Not done: press it yourself", code: "submit_blocked" });
  assert.deepEqual(blocked.actions, ["dismiss"], "a held-back submit is not handed to the coder");
});

test("a coder card shows the goal and why, and says it is queued, not started", () => {
  const v = (extra) => P.view(Object.assign({ id: "c", kind: "coder", goal: "Rename the files", why: "Needs the shell", state: "pending", untrustedPage: true }, extra));
  const pending = v();
  assert.deepEqual([pending.title, pending.notes, pending.actions, pending.labels.coder], ["Rename the files", "Needs the shell", ["coder", "dismiss"], "Hand to coder"]);
  assert.match(pending.warning, /Check it before you hand it over/);
  assert.equal(v({ state: "queued" }).status, "Queued — press Start in the MonoAgent app.");
  assert.deepEqual(v({ state: "queued" }).actions, []);
});

// A tiny DOM: just what the card builder calls, and no innerHTML at all.
class Node_ {
  constructor(tag) { this.tag = tag; this.children = []; this.dataset = {}; this.listeners = {}; this._text = ""; this.className = ""; this.type = ""; this.attrs = {}; }
  appendChild(c) { this.children.push(c); return c; }
  setAttribute(k, v) { this.attrs[k] = String(v); }
  addEventListener(t, fn) { (this.listeners[t] = this.listeners[t] || []).push(fn); }
  set textContent(v) { this._text = String(v); this.children = []; }
  get textContent() { return this._text + this.children.map((c) => c.textContent).join(""); }
  set innerHTML(v) { throw new Error("innerHTML was used"); }
  all() { return [this, ...this.children.flatMap((c) => c.all())]; }
}
const doc = { createElement: (t) => new Node_(t) };

test("action and coder cards are text nodes, and their buttons call back only when pressed", () => {
  const calls = [];
  const on = { run: () => calls.push("run"), coder: () => calls.push("coder"), dismiss: () => calls.push("dismiss") };
  const evil = "<img src=x onerror=alert(1)>";
  const a = P.card(doc, { id: "a", kind: "action", action: "type", target: evil, text: evil, url: "", untrustedPage: true, state: "failed", feedback: `Not done: ${evil}`, code: "not_found" }, on);
  assert.equal(a.dataset.kind, "action");
  assert.equal(a.dataset.untrusted, "true");
  assert.match(a.textContent, /<img src=x onerror=alert\(1\)>/, "shown as text");
  assert.ok(!a.all().some((n) => n.tag === "img"));
  const buttons = a.all().filter((n) => n.tag === "button");
  assert.deepEqual(buttons.map((b) => b.textContent), ["Try again", "Hand to coder", "Dismiss"]);
  assert.deepEqual(calls, [], "nothing happens until a button is pressed");
  buttons[1].listeners.click[0]({});
  assert.deepEqual(calls, ["coder"]);
  const c = P.card(doc, { id: "c", kind: "coder", goal: evil, why: evil, untrustedPage: false, state: "pending" }, on);
  assert.deepEqual(c.all().filter((n) => n.tag === "button").map((b) => b.textContent), ["Hand to coder", "Dismiss"]);
  assert.ok(!c.all().some((n) => n.tag === "img"));
});
