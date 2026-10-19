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
 * Kinds, and what a click does (sidepanel_chat_cards.js):
 *   task       title and notes -> task.add kind "chat" (task_bridge.js), which
 *              MonoAgent files in the Inbox as the agent agent:extension-chat.
 *   recording  goal and steps -> Start recording starts the Record section's
 *              recorder on this tab; the card then follows that recording
 *              (recordingId) to Review and save, and the automation is saved
 *              only by the Record section's own Save.
 * This table is the seam the vault save plugs into once the bridge has
 * vault.save: its tool in TOOLS, its kind in KINDS, and the worker message
 * the panel sends on its click.
 *
 * A card made in a conversation that has read a page carries untrustedPage,
 * and says the page may have steered it.
 *
 *   fromTool(name, payload, id, readPage)  -> a card with a new nonce, or null
 *   clone(list), settle(state, key, patch), keyOf(p), clientId(p), afterAdd(answer), view(p)  pure
 *   card(doc, p, {add, dismiss})            DOM, no innerHTML
 */

(function (root) {
  "use strict";

  const TITLE_RUNES = 200;
  const PAGE_URL_CHARS = 2048;
  const NOTES_BYTES = 4000;

  // The proposal tools, by the kind each answers with.
  const TOOLS = { propose_task: "task", propose_recording: "recording" };

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

  // Control, bidi and zero-width characters: no place in a goal that the
  // recorder and its analysis read.
  const CONTROLS = /[\u0000-\u001f\u007f-\u009f\u061c\u200b-\u200f\u202a-\u202e\u2060-\u2069\ufeff]/g;

  /** cleanGoal is a recording goal without markup or control characters, one line, 200 runes. */
  function cleanGoal(v) {
    return runes(oneLine(str(v).replace(/<[^>]*>/g, " ").replace(/[<>]/g, " ").replace(CONTROLS, " ")), TITLE_RUNES);
  }

  /** pageOf is the page a card was proposed on ({tabId, url}), checked; null when there is none. */
  function pageOf(v) {
    if (!v || !Number.isInteger(v.tabId) || v.tabId <= 0 || typeof v.url !== "string" || v.url.length > PAGE_URL_CHARS) return null;
    return { tabId: v.tabId, url: v.url };
  }

  const RECORDING_STATUS = {
    sending: "Starting…",
    recording: "Recording in the Record section below: do the steps on the page, then press Stop there.",
    recorded: "Recorded.",
    reviewing: "Analyzing the recording…",
    empty: "Nothing was recorded.",
    dismissed: "Not recorded",
  };

  // What each kind keeps of an answer, and when it is a card at all; its
  // states, and what a card in each one says and offers (screen).
  const KINDS = {
    task: {
      heading: "Add a task?",
      fields: (p) => ({ title: runes(oneLine(p.title), TITLE_RUNES), notes: bytes(str(p.notes).trim(), NOTES_BYTES) }),
      valid: (f) => !!f.title,
      text: (p) => ({ title: p.title, notes: p.notes || "" }),
      check: "Check it before you add it.",
      states: new Set(["pending", "sending", "added", "queued", "refused", "dismissed"]),
      screen: (p) => {
        const status = { sending: "Adding…", dismissed: "Not added" }[p.state];
        return {
          actions: p.state === "pending" ? ["add", "dismiss"] : [],
          labels: { add: "Add to Inbox", dismiss: "Dismiss" },
          status: status !== undefined ? status : p.feedback || "",
        };
      },
    },
    recording: {
      heading: "Record these steps?",
      fields: (p) => ({ goal: cleanGoal(p.goal), steps: bytes(str(p.steps).trim(), NOTES_BYTES) }),
      valid: (f) => !!f.goal,
      text: (p) => ({ title: p.goal, notes: p.steps || "" }),
      check: "Check the goal before you record.",
      states: new Set(["pending", "sending", "recording", "recorded", "reviewing", "saved", "empty", "refused", "dismissed"]),
      screen: (p) => {
        const again = p.state === "empty" || p.state === "refused";
        const before = p.state === "pending" || again;
        return {
          actions: before ? ["add", "dismiss"] : p.state === "recorded" ? ["review"] : [],
          labels: { add: again ? "Record again" : "Start recording", dismiss: "Dismiss", review: "Review and save" },
          status: p.feedback || RECORDING_STATUS[p.state] || "",
          // Before Start the goal is a field: the person sees it and can change it.
          edit: before ? { label: "What you will do", value: p.goal } : undefined,
        };
      },
    },
  };

  // A recording's id as the recorder makes it (rec-<stamp>-<rand>).
  const usableRecording = (id) => typeof id === "string" && /^[A-Za-z0-9_-]{1,80}$/.test(id);
  // The recording card's states that need the recording they follow.
  const FOLLOWING = new Set(["recording", "recorded", "reviewing"]);

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
   * nothing twice. A recording card keeps the recording it follows; one cut
   * off mid-review (its draft went with the panel) offers Review again, and
   * one that lost its recording starts over.
   */
  function clone(list) {
    const out = [];
    for (const p of Array.isArray(list) ? list : []) {
      const kind = p && KINDS[p.kind];
      let state = kind && kind.states.has(p.state) && p.state !== "sending" ? p.state : "pending";
      if (state === "reviewing") state = "recorded";
      const c = cardOf(p, p && p.id, p && p.untrustedPage, state, p && p.feedback);
      if (!c) continue;
      if (NONCE.test(p.nonce)) c.nonce = p.nonce;
      const page = pageOf(p.page);
      if (page) c.page = page;
      if (c.kind === "recording" && usableRecording(p.recordingId)) c.recordingId = p.recordingId;
      else if (FOLLOWING.has(c.state)) Object.assign(c, { state: "pending", feedback: "" });
      out.push(c);
    }
    return out;
  }

  /**
   * keyOf names one card among all of a conversation's: its nonce. Call ids
   * are not enough, as some runtimes number their calls per turn
   * ("functions.propose_task:0"). A card without a nonce is known by its id.
   */
  const keyOf = (p) => (p && NONCE.test(p.nonce) ? p.nonce : str(p && p.id));

  /**
   * settle records what became of a card ({state, feedback}, and a recording
   * card's recordingId), found by its key; true when there was one. A state
   * the card's kind does not have is not taken.
   */
  function settle(chatState, key, patch) {
    for (const m of (chatState && chatState.messages) || []) {
      const p = (m.proposals || []).find((x) => keyOf(x) === key);
      if (!p) continue;
      const kind = KINDS[p.kind];
      if (patch && kind && kind.states.has(patch.state)) p.state = patch.state;
      if (patch && typeof patch.feedback === "string") p.feedback = patch.feedback.slice(0, 300);
      if (patch && p.kind === "recording" && usableRecording(patch.recordingId)) p.recordingId = patch.recordingId;
      if (patch && p.kind === "recording" && cleanGoal(patch.goal)) p.goal = cleanGoal(patch.goal);
      return true;
    }
    return false;
  }

  /** profileTag is 8 hex digits of a profile id (FNV-1a), to scope a task id to it. */
  function profileTag(profile) {
    let h = 0x811c9dc5;
    for (const b of new TextEncoder().encode(str(profile))) h = Math.imul(h ^ b, 0x01000193) >>> 0;
    return h.toString(16).padStart(8, "0");
  }

  /**
   * clientId is the id a card's task is sent under, for good (task_outbox.js):
   * its nonce, the profile it goes to, then its call id for whoever reads the
   * board. The board takes a repeated id as the same task, so a resend adds
   * nothing twice; scoped to the profile, a retry under another profile is
   * another id, never one id in two profiles' entries. A card stored before
   * cards had a nonce keeps the id it was sent under.
   */
  function clientId(p, profile) {
    const call = str(p && p.id).replace(/[^A-Za-z0-9_-]/g, "");
    if (!NONCE.test(p && p.nonce)) return `chat-${call}`.slice(0, 64);
    return (profile ? `chat-${p.nonce}-${profileTag(profile)}-${call}` : `chat-${p.nonce}-${call}`).slice(0, 64);
  }

  /**
   * afterAdd is what a task card becomes once its Add is answered
   * (task_bridge.js add): added or queued for good; refused, full or empty
   * final, as trying again cannot help; pending again with the reason when
   * there was no profile or no answer at all, so the person can retry.
   */
  function afterAdd(answer) {
    const a = answer || {};
    const text = (a.feedback && a.feedback.text) || a.error || "";
    if (a.status === "added" || a.status === "queued") return { state: a.status, feedback: text };
    if (a.status === "refused" || a.status === "full" || a.status === "empty") return { state: "refused", feedback: text || "Not added" };
    return { state: "pending", feedback: text || "Not added: try again" };
  }

  /** view is what a card says, from its state: its buttons are actions, named by labels. */
  function view(p) {
    const k = KINDS[p.kind] || KINDS.task;
    return Object.assign(
      {
        heading: k.heading,
        warning: p.untrustedPage ? `Suggested after reading a web page, which can steer the chat. ${k.check}` : "",
      },
      k.text(p),
      k.screen(p)
    );
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
    let field = null;
    if (v.edit) {
      field = doc.createElement("input");
      field.type = "text";
      field.className = "chat-proposal-goal";
      field.value = v.edit.value;
      field.setAttribute("aria-label", v.edit.label);
      box.appendChild(field);
    } else {
      line("chat-proposal-title", v.title);
    }
    if (v.notes) line("chat-proposal-notes", v.notes);
    if (v.warning) line("chat-proposal-warning", v.warning);
    // What happened so far, then what can be done next (a recording card has both).
    if (v.status) line("chat-proposal-status", v.status).setAttribute("role", "status");
    if (v.actions.length) {
      const row = doc.createElement("div");
      row.className = "chat-proposal-actions";
      const button = (label, fn, cls) => {
        const b = doc.createElement("button");
        b.type = "button";
        b.className = cls;
        b.textContent = label;
        // A field's value goes with the click: the goal as the person left it.
        b.addEventListener("click", () => fn && fn(field ? field.value : undefined));
        row.appendChild(b);
      };
      for (const a of v.actions) {
        button(v.labels[a], on && on[a], a === "dismiss" ? "btn-secondary btn-tiny" : "btn-secondary btn-tiny chat-proposal-add");
      }
      box.appendChild(row);
    }
    return box;
  }

  root.MonoChatProposals = { fromTool, clone, settle, keyOf, clientId, afterAdd, view, card, cleanGoal, pageOf, TOOLS, KINDS, TITLE_RUNES, NOTES_BYTES };
})(globalThis);
