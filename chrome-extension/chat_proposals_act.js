/**
 * MonoAgent Bridge — the chat's two "do something" proposal kinds
 *
 * Read by chat_proposals.js, which loads after this file and folds TOOLS and
 * KINDS into its own tables, so both are checked, stored, settled and drawn
 * exactly as the task, recording and vault cards are. The answer is a
 * model's, steered by whatever page it read: every field is clamped and
 * stripped again here.
 *
 *   action   propose_page_action: click, double_click, type or open_url on
 *            the page the person is looking at. Run (page_action_bridge.js)
 *            does it, and only on their click.
 *   coder    propose_coder_handoff: a goal for the coder. Hand to coder
 *            queues a coder chat (coder_bridge.js); the person presses Start
 *            in the MonoAgent app. The extension never starts the turn.
 *
 * Each kind: heading, fields(p) (what is kept of an answer), valid(f), text(p)
 * (the plain words), check, states and screen(p) (the buttons and status),
 * the same shape chat_proposals.js documents.
 */

(function (root) {
  "use strict";

  const TARGET_RUNES = 200;
  const TEXT_RUNES = 2000;
  const URL_CHARS = 2048;
  const GOAL_RUNES = 2000;
  const WHY_RUNES = 300;
  const ACTIONS = new Set(["click", "double_click", "type", "open_url"]);

  const str = (v) => (typeof v === "string" ? v : "");
  const runes = (s, n) => [...str(s)].slice(0, n).join("");
  const oneLine = (s) => str(s).split(/\s+/).filter(Boolean).join(" ");
  // Controls, format characters (bidi, zero-width), private-use, unassigned,
  // lone surrogates and the invisible tag block.
  const HIDDEN = /[\p{Cc}\p{Cf}\p{Co}\p{Cn}\p{Cs}\u{E0000}-\u{E007F}]/gu;
  const unseen = (s) => str(s).replace(HIDDEN, " ");
  const line = (v, n) => runes(oneLine(unseen(v)), n);
  // Typed text keeps its line breaks and nothing else invisible.
  const typed = (v) => runes(str(v).replace(/\r\n?/g, "\n").replace(/[^\S\n]/g, " ").split("\n").map(unseen).join("\n"), TEXT_RUNES);

  /** httpUrl is an absolute http(s) address, no spaces, not too long; else "". */
  function httpUrl(v) {
    const s = str(v).trim();
    if (!s || s.length > URL_CHARS || /[\s\u0000-\u001f\u007f]/.test(s)) return "";
    try {
      const u = new URL(s);
      return (u.protocol === "http:" || u.protocol === "https:") && u.host ? s : "";
    } catch {
      return "";
    }
  }

  const hostOf = (url) => {
    try {
      return new URL(url).hostname;
    } catch {
      return "";
    }
  };
  const privateHost = (host) => !!(root.MonoPrivateNet && root.MonoPrivateNet.isPrivateHost(host));
  // The target as the card says it: quoted, and short.
  const quoted = (v) => `“${runes(v, 80)}${[...str(v)].length > 80 ? "…" : ""}”`;

  const TOOLS = { propose_page_action: "action", propose_coder_handoff: "coder" };
  const UNKNOWN = "Unknown outcome \u2014 check the page";
  const QUEUED = "Queued — press Start in the MonoAgent app.";

  const KINDS = {
    action: {
      heading: "Do this on the page?",
      fields: (p) => {
        const action = ACTIONS.has(p.action) ? p.action : "";
        // The target is clamped like a recording goal: no markup, no hidden characters.
        const clean = root.MonoChatProposals ? root.MonoChatProposals.cleanGoal : (v) => line(v, TARGET_RUNES);
        return {
          action,
          target: action && action !== "open_url" ? clean(p.target) : "",
          text: action === "type" ? typed(p.text) : "",
          url: action === "open_url" ? httpUrl(p.url) : "",
        };
      },
      valid: (f) => (f.action === "open_url" ? !!f.url : !!f.action && !!f.target && (f.action !== "type" || !!f.text)),
      text: (p) => {
        const words = {
          click: `Click ${quoted(p.target)} on this page`,
          double_click: `Double-click ${quoted(p.target)} on this page`,
          type: `Type into ${quoted(p.target)} on this page (replaces what is in the field)`,
          open_url: `Open ${hostOf(p.url)} in a new tab`,
        };
        const out = { title: words[p.action] || "" };
        if (p.action === "type") out.notes = p.text;
        if (p.action === "open_url") {
          out.notes = p.url;
          if (privateHost(hostOf(p.url))) out.source = `${hostOf(p.url)} is a private network address`;
        }
        return out;
      },
      check: "Check it before you run it.",
      states: new Set(["pending", "sending", "done", "failed", "unknown", "queued", "dismissed"]),
      screen: (p) => {
        const blocked = p.state === "failed" && p.code === "submit_blocked";
        // An unknown outcome may have happened: no Run again, only a look at the page.
        const actions = { pending: ["run", "dismiss"], failed: blocked ? ["dismiss"] : ["run", "coder", "dismiss"], unknown: ["coder", "dismiss"] }[p.state] || [];
        const status = { sending: "Running…", dismissed: "Not run", done: p.feedback || "Done.", unknown: p.feedback || UNKNOWN }[p.state];
        return {
          actions,
          labels: { run: p.state === "failed" ? "Try again" : "Run", coder: "Hand to coder", dismiss: "Dismiss" },
          status: status !== undefined ? status : p.feedback || "",
        };
      },
    },
    coder: {
      heading: "Hand this to the coder?",
      fields: (p) => ({ goal: line(p.goal, GOAL_RUNES), why: line(p.why, WHY_RUNES) }),
      valid: (f) => !!f.goal,
      text: (p) => ({ title: p.goal, notes: p.why || "" }),
      check: "Check it before you hand it over.",
      states: new Set(["pending", "sending", "queued", "dismissed"]),
      screen: (p) => {
        const status = { sending: "Queuing…", dismissed: "Not handed over", queued: p.feedback || QUEUED }[p.state];
        return {
          actions: p.state === "pending" ? ["coder", "dismiss"] : [],
          labels: { coder: "Hand to coder", dismiss: "Dismiss" },
          status: status !== undefined ? status : p.feedback || "",
        };
      },
    },
  };

  root.MonoChatProposalKinds = { TOOLS, KINDS, QUEUED, UNKNOWN, httpUrl, TARGET_RUNES, TEXT_RUNES, GOAL_RUNES, WHY_RUNES, URL_CHARS };
})(globalThis);
