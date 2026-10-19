/**
 * MonoAgent Bridge — the side panel chat's proposals (issue #451)
 *
 * The chat may suggest a change, never make one. A proposal tool
 * (propose_task, internal/ai/chat/monoagent_tools_proposals.go) writes
 * nothing: it answers {"proposal":{"kind":…},"added":false}, chat_core.js
 * turns that answer into a card under the reply, and only the person's click
 * on the card sends it on. Each card is checked again here: the answer is a
 * model's, steered by whatever page it read.
 *
 * Kinds, and what a click does:
 *   task   title and notes -> task.add kind "chat" (task_bridge.js), which
 *          MonoAgent files in the Inbox as the agent agent:extension-chat.
 * This table is the seam the vault save plugs into once the bridge has
 * vault.save: its tool in TOOLS, its kind in KINDS, and the worker message
 * the panel sends on its click.
 *
 * A card made in a conversation that has read a page carries untrustedPage,
 * and says the page may have steered it.
 *
 *   fromTool(name, payload, id, readPage)  -> a card with a new nonce, or null
 *   clone(list), settle(state, id, patch), clientId(p), view(p)  pure
 *   card(doc, p, {add, dismiss})            DOM, no innerHTML
 */

(function (root) {
  "use strict";

  const TITLE_RUNES = 200;
  const NOTES_BYTES = 4000;

  // The proposal tools, by the kind each answers with.
  const TOOLS = { propose_task: "task" };

  const str = (v) => (typeof v === "string" ? v : "");
  const oneLine = (s) => str(s).split(/\s+/).filter(Boolean).join(" ");
  const runes = (s, n) => [...str(s)].slice(0, n).join("");
  function bytes(s, n) {
    let out = "";
    let used = 0;
    for (const ch of str(s)) {
      const size = new TextEncoder().encode(ch).length;
      if (used + size > n) break;
      out += ch;
      used += size;
    }
    return out;
  }

  // What each kind keeps of an answer, and when it is a card at all.
  const KINDS = {
    task: {
      heading: "Add a task?",
      confirm: "Add to Inbox",
      fields: (p) => ({ title: runes(oneLine(p.title), TITLE_RUNES), notes: bytes(str(p.notes).trim(), NOTES_BYTES) }),
      valid: (f) => !!f.title,
    },
  };

  const STATES = new Set(["pending", "sending", "added", "queued", "refused", "dismissed"]);

  // Each card's own random key, made with it and kept with it: its task's id
  // (clientId) is never another card's, however the call ids compare.
  const NONCE = /^[0-9a-f]{16}$/;
  function newNonce() {
    const bytes = new Uint8Array(8);
    crypto.getRandomValues(bytes);
    return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  }

  /** cardOf checks one proposal (an answer's, or a stored card) and returns the card, or null. */
  function cardOf(raw, id, untrustedPage, state, feedback) {
    const kind = raw && KINDS[raw.kind] ? raw.kind : "";
    if (!kind || !str(id)) return null;
    const fields = KINDS[kind].fields(raw);
    if (!KINDS[kind].valid(fields)) return null;
    return Object.assign({ id: str(id), kind }, fields, { untrustedPage: !!untrustedPage, state, feedback: str(feedback).slice(0, 300) });
  }

  /**
   * fromTool reads the card a finished proposal call answered with: name is
   * the tool's, payload the tool.completed event's, id its call id, readPage
   * whether the conversation has read a page. null for any other tool, or an
   * answer that is not a proposal of the tool's own kind.
   */
  function fromTool(name, payload, id, readPage) {
    const kind = TOOLS[str(name)];
    if (!kind) return null;
    let res;
    try {
      res = JSON.parse(str(payload && payload.result));
    } catch {
      return null;
    }
    const p = res && res.proposal;
    if (!p || p.kind !== kind) return null;
    const card = cardOf(p, id, readPage, "pending", "");
    if (card) card.nonce = newNonce();
    return card;
  }

  /**
   * clone checks stored cards again. A card left "sending" by a panel that
   * closed mid-send is pending again: the task's id makes a second send add
   * nothing twice.
   */
  function clone(list) {
    const out = [];
    for (const p of Array.isArray(list) ? list : []) {
      const state = STATES.has(p && p.state) && p.state !== "sending" ? p.state : "pending";
      const c = cardOf(p, p && p.id, p && p.untrustedPage, state, p && p.feedback);
      if (c && NONCE.test(p.nonce)) c.nonce = p.nonce;
      if (c) out.push(c);
    }
    return out;
  }

  /** settle records what became of a card ({state, feedback}); true when there was one. */
  function settle(chatState, id, patch) {
    for (const m of (chatState && chatState.messages) || []) {
      const p = (m.proposals || []).find((x) => x.id === id);
      if (!p) continue;
      if (patch && STATES.has(patch.state)) p.state = patch.state;
      if (patch && typeof patch.feedback === "string") p.feedback = patch.feedback.slice(0, 300);
      return true;
    }
    return false;
  }

  /**
   * clientId is the id a card's task is sent under, for good (task_outbox.js):
   * its nonce, then its call id for whoever reads the board. The board takes
   * a repeated id as the same task, so a resend adds nothing twice. A card
   * stored before cards had a nonce keeps the id it was sent under.
   */
  function clientId(p) {
    const call = str(p && p.id).replace(/[^A-Za-z0-9_-]/g, "");
    return (NONCE.test(p && p.nonce) ? `chat-${p.nonce}-${call}` : `chat-${call}`).slice(0, 64);
  }

  /** view is what a card says, from its state. */
  function view(p) {
    const k = KINDS[p.kind] || KINDS.task;
    const status = {
      pending: "",
      sending: "Adding…",
      dismissed: "Not added",
    }[p.state];
    return {
      heading: k.heading,
      title: p.title,
      notes: p.notes || "",
      warning: p.untrustedPage ? "Suggested after reading a web page, which can steer the chat. Check it before you add it." : "",
      actions: p.state === "pending" ? ["add", "dismiss"] : [],
      confirm: k.confirm,
      status: status !== undefined ? status : p.feedback || "",
    };
  }

  /** card builds a card's DOM: text only, and a button calls back only when pressed. */
  function card(doc, p, on) {
    const v = view(p);
    const box = doc.createElement("div");
    box.className = "chat-proposal";
    box.dataset.state = p.state;
    box.dataset.kind = p.kind;
    if (p.untrustedPage) box.dataset.untrusted = "true";
    const line = (cls, text) => {
      const el = doc.createElement("div");
      el.className = cls;
      el.textContent = text;
      box.appendChild(el);
      return el;
    };
    line("chat-proposal-heading", v.heading);
    line("chat-proposal-title", v.title);
    if (v.notes) line("chat-proposal-notes", v.notes);
    if (v.warning) line("chat-proposal-warning", v.warning);
    if (v.actions.length) {
      const row = doc.createElement("div");
      row.className = "chat-proposal-actions";
      const button = (label, fn, cls) => {
        const b = doc.createElement("button");
        b.type = "button";
        b.className = cls;
        b.textContent = label;
        b.addEventListener("click", () => fn && fn());
        row.appendChild(b);
      };
      button(v.confirm, on && on.add, "btn-secondary btn-tiny chat-proposal-add");
      button("Dismiss", on && on.dismiss, "btn-secondary btn-tiny");
      box.appendChild(row);
    }
    if (v.status) line("chat-proposal-status", v.status).setAttribute("role", "status");
    return box;
  }

  root.MonoChatProposals = { fromTool, clone, settle, clientId, view, card, TOOLS, KINDS, TITLE_RUNES, NOTES_BYTES };
})(globalThis);
