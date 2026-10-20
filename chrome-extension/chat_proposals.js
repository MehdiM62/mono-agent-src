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
 *   vault      an item (an address on the page, or the selection) and a name
 *              -> Save to vault (vault_bridge.js), which fetches the item only
 *              if it is on the page the person is on, and sends vault.save.
 *   action     (chat_proposals_act.js) a click, double-click, typing or a new
 *              tab on this page -> Run (page_action_bridge.js).
 *   coder      (chat_proposals_act.js) a goal -> Hand to coder queues a coder
 *              chat (coder_bridge.js); the person starts it in the app.
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
  const NOTES_BYTES = 4000;

  // The proposal tools, by the kind each answers with.
  // The two "do something" kinds (propose_page_action, propose_coder_handoff) come from
  // chat_proposals_act.js, loaded first; without it they are simply not cards.
  const Act = root.MonoChatProposalKinds || { TOOLS: {}, KINDS: {} };
  const TOOLS = Object.assign({ propose_task: "task", propose_recording: "recording", propose_vault_save: "vault" }, Act.TOOLS);
  const NAME_BYTES = 255;
  const URL_CHARS = 2048;

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

  /** itemUrl is a vault item's address: absolute http(s), no spaces, not too long; else "". */
  function itemUrl(v) {
    const s = str(v).trim();
    if (!s || s.length > URL_CHARS || /[\s\u0000-\u001f\u007f]/.test(s)) return "";
    try {
      const u = new URL(s);
      return (u.protocol === "http:" || u.protocol === "https:") && u.host ? s : "";
    } catch {
      return "";
    }
  }

  // Which hosts are on the person's own network (private_net.js, loaded first).
  const privateHost = (host) => !!(root.MonoPrivateNet && root.MonoPrivateNet.isPrivateHost(host));

  const hostOf = (url) => {
    try {
      return new URL(url).hostname;
    } catch {
      return "";
    }
  };

  /** fileOf is the last part of an address's path, readable: what a file there is called. */
  function fileOf(url) {
    try {
      const last = new URL(url).pathname.split("/").filter(Boolean).pop() || "";
      return decodeURIComponent(last);
    } catch {
      return "";
    }
  }

  // What shows nothing on screen but the recorder and its analysis read:
  // controls, format characters (bidi, zero-width, the byte order mark),
  // private-use, unassigned and lone surrogate code points, and the Unicode
  // tag block (invisible "ASCII smuggling").
  const HIDDEN = /[\p{Cc}\p{Cf}\p{Co}\p{Cn}\p{Cs}\u{E0000}-\u{E007F}]/u;
  const SELECTORS = /[\u{180B}-\u{180D}\u{180F}\u{FE00}-\u{FE0F}\u{E0100}-\u{E01EF}]/u;
  // The same character classes as Go's recording.cleanLine (privacy.go).
  const EMOJI = /[\u00a9\u00ae\u203c\u2049\u2122\u2139\u24c2\u3030\u303d\u3297\u3299\u2190-\u21ff\u2300-\u23ff\u25a0-\u27bf\u2900-\u297f\u2b00-\u2bff\u{1F000}-\u{1FAFF}]/u;
  const VIRAMA = /[\u094d\u09cd\u0a4d\u0acd\u0b4d\u0bcd\u0c4d\u0ccd\u0d4d\u0dca\u0e3a\u1039\u103a\u17d2\u1a60\u1b44\ua8c4\ua9c0\u{11046}]/u;
  const JOIN_SCRIPT = /[\u0600-\u06ff\u0750-\u077f\u08a0-\u08ff\ufb50-\ufdff\ufe70-\ufeff\u0900-\u0dff]/u;
  const LETTER = /\p{L}/u;
  const DIRECTION = /[\u200e\u200f\u061c]/u;
  const RTL = /[\u0590-\u08ff\ufb1d-\ufdff\ufe70-\ufeff\u{10800}-\u{10fff}\u{1e800}-\u{1efff}]/u;
  const rtl = (c) => LETTER.test(c) && RTL.test(c);
  const keycap = (base, next) => next === "\u20e3" && /[0-9#*]/.test(base);

  /**
   * cleanGoal is a recording goal as the recorder and its analysis get it:
   * no markup and no hidden characters (HIDDEN), one line, 200 runes. What
   * text needs stays, and no more, so what is kept carries no hidden bits: a
   * ZWJ that joins emoji, or a conjunct after a virama or coeng; a ZWNJ
   * inside an Arabic-script or Indic word; VS16 that makes an emoji, and VS15
   * or VS16 on a keycap; a direction mark where the text turns between
   * right-to-left and left-to-right letters, one per turn. A soft hyphen, and a
   * joiner or selector that is not needed, are deleted. The same rules as
   * Go's recording.CleanGoal.
   */
  function cleanGoal(v) {
    const cps = [...str(v).replace(/<[^>]*>/g, " ").replace(/[<>]/g, " ")];
    const out = [];
    const last = () => (out.length ? out[out.length - 1] : " ");
    // The last kept character but a variation selector: what a ZWJ after a heart and VS16 joins.
    const lastBase = () => [...out].reverse().find((c) => c !== "\ufe0f" && c !== "\ufe0e") || " ";
    // The direction of the last letter kept (the start counts as left-to-right),
    // and whether a direction mark was kept since it; the text's end counts as
    // left-to-right too.
    let lastRTL = false;
    let markSinceLetter = false;
    const nextLetterRTL = (i) => {
      const letter = cps.slice(i + 1).find((c) => LETTER.test(c));
      return !!letter && rtl(letter);
    };
    cps.forEach((c, i) => {
      const next = cps[i + 1] || " ";
      let keep = true;
      if (c === "\u00ad") keep = false; // a soft hyphen only says where a word may break
      else if (c === "\u200d") keep = (EMOJI.test(lastBase()) && EMOJI.test(next)) || (VIRAMA.test(last()) && LETTER.test(next));
      else if (c === "\u200c") keep = JOIN_SCRIPT.test(last()) && JOIN_SCRIPT.test(next) && LETTER.test(next);
      else if (DIRECTION.test(c)) {
        keep = !markSinceLetter && lastRTL !== nextLetterRTL(i);
        markSinceLetter = markSinceLetter || keep;
      }
      else if (c === "\ufe0f") keep = EMOJI.test(last()) || keycap(last(), next);
      else if (c === "\ufe0e") keep = keycap(last(), next);
      else if (SELECTORS.test(c)) keep = false;
      else if (HIDDEN.test(c)) {
        out.push(" ");
        return;
      }
      if (keep) out.push(c);
      if (keep && LETTER.test(c)) {
        lastRTL = rtl(c);
        markSinceLetter = false;
      }
    });
    return runes(oneLine(out.join("")), TITLE_RUNES);
  }

  /** pageOf is the page a card was proposed on ({tabId, url}), checked; null when there is none. */
  function pageOf(v) {
    if (!v || !Number.isInteger(v.tabId) || v.tabId <= 0 || typeof v.url !== "string" || v.url.length > URL_CHARS) return null;
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

  KINDS.vault = {
    heading: "Save to your vault?",
    fields: (p) => {
      const url = itemUrl(p.url);
      return { url, selection: !url && p.selection === true, name: bytes(oneLine(p.name), NAME_BYTES) };
    },
    valid: (f) => !!f.url || f.selection,
    text: (p) => {
      const host = hostOf(p.url);
      return {
        title: p.name || fileOf(p.url) || (p.selection ? "Your selection" : "A file from this page"),
        // Where it would come from, plainly: a page can embed an address on the person's own network.
        source: !p.url ? "from the page you are on" : privateHost(host) ? `from ${host}, a private network address` : `from ${host}`,
        notes: p.url || "The text you have selected on this page, as a text file",
      };
    },
    check: "Check the file before you save it.",
    states: new Set(["pending", "sending", "saved", "refused", "dismissed"]),
    screen: (p) => {
      const status = { sending: "Saving…", dismissed: "Not saved" }[p.state];
      return {
        actions: p.state === "pending" ? ["add", "dismiss"] : [],
        labels: { add: "Save to vault", dismiss: "Dismiss" },
        status: status !== undefined ? status : p.feedback || "",
      };
    },
  };

  Object.assign(KINDS, Act.KINDS);

  // A failed action card's reason (submit_blocked, not_found, ...), as the worker names it.
  const usableCode = (c) => typeof c === "string" && /^[a-z_]{1,40}$/.test(c);

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
      let feedback = p && p.feedback;
      // A Run cut off mid-send may have happened: it is never offered again.
      if (p && p.kind === "action" && p.state === "sending") {
        state = "unknown";
        feedback = Act.UNKNOWN;
      }
      const c = cardOf(p, p && p.id, p && p.untrustedPage, state, feedback);
      if (!c) continue;
      if (NONCE.test(p.nonce)) c.nonce = p.nonce;
      const page = pageOf(p.page);
      if (page) c.page = page;
      if (c.kind === "action" && usableCode(p.code)) c.code = p.code;
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
      if (patch && p.kind === "action" && "code" in patch) p.code = usableCode(patch.code) ? patch.code : undefined;
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
   * afterAdd is what a task or vault card becomes once its click is
   * answered (task_bridge.js add, vault_bridge.js save): added, queued or
   * saved for good; refused, full or empty final, as trying again cannot
   * help; pending again with the reason otherwise (no profile, no answer,
   * a save to retry), so the person can press it again.
   */
  function afterAdd(answer) {
    const a = answer || {};
    const text = (a.feedback && a.feedback.text) || a.error || "";
    if (a.status === "added" || a.status === "queued" || a.status === "saved") return { state: a.status, feedback: text };
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
    if (v.source) line("chat-proposal-source", v.source);
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

  root.MonoChatProposals = { fromTool, clone, settle, keyOf, clientId, afterAdd, view, card, cleanGoal, pageOf, QUEUED: Act.QUEUED, UNKNOWN: Act.UNKNOWN, TOOLS, KINDS, TITLE_RUNES, NOTES_BYTES };
})(globalThis);
