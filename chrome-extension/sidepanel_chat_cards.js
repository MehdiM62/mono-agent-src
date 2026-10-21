/**
 * MonoAgent Bridge — what a click on a side panel chat card does (issue #451)
 *
 * The chat proposes; only the person's click on a card (chat_proposals.js)
 * does anything, and this is what each click does:
 *
 *   task       Add sends it to the worker (task_add_proposal), which files it
 *              in the Inbox as agent:extension-chat.
 *   recording  Start recording starts the Record section's own recorder
 *              (sidepanel_record.js, MonoPanelRecord) on this tab with the
 *              card's goal. The card follows that recording (follow): once
 *              it is delivered, Review and save runs its analysis, and the
 *              draft opens in the Record section, whose Save is the only way
 *              it is saved. Nothing here saves an automation.
 *   vault      Save to vault asks the worker (vault_save_proposal) to fetch
 *              the card's item from the panel's page, if it is there, and
 *              send it as vault.save.
 *   action     Run sends page_action_run to the worker (page_action_bridge.js),
 *              which does it in the tab the card was proposed on, if that
 *              page is still there. A failed card also offers Hand to coder; one whose outcome is unknown (no
 *              answer, or cut off) is never run again.
 *   coder      Hand to coder queues a coder chat (coder_queue, kind "ask") for
 *              this tab with the card's goal and why, and nothing else: the
 *              person presses Start in the MonoAgent app, never the extension.
 *
 *   create(deps) -> { confirm(key), dismiss(key), review(key), run(key), coder(key), follow(event) }
 *
 * A card is named by its key (MonoChatProposals.keyOf), not its call id:
 * two turns' cards can share a call id.
 *
 * deps: chat() the conversation on screen, ask(message) the worker, profile(),
 * current() a token that changes when the panel switches profile, save(),
 * draw(), acted() (a page step ran: share the page again), record (MonoPanelRecord, or null), page() the panel's page
 * ({tabId, url}): a vault card saves an item on it, and a recording card
 * records the page it was proposed on, only while the panel shows it.
 */

(function (root) {
  "use strict";

  const Proposals = root.MonoChatProposals;

  function create(deps) {
    const cardsOf = () => (deps.chat().messages || []).flatMap((m) => m.proposals || []);
    const keyOf = Proposals.keyOf;
    const find = (key) => cardsOf().find((p) => keyOf(p) === key) || null;
    const errorOf = (err) => (err && err.message) || String(err);
    const tabOf = (page) => (page && page.tabId ? { tabId: page.tabId, url: page.url || "" } : null);

    async function settle(key, patch, mine) {
      if (mine !== undefined && mine !== deps.current()) return; // another profile's chat is on screen now
      Proposals.settle(deps.chat(), key, patch);
      await deps.save();
      deps.draw();
    }

    // value: what the card's field says (a recording's goal, as the person left it).
    async function confirm(key, value) {
      const p = find(key);
      if (!p) return;
      if (p.kind === "recording") return startRecording(p, value);
      if (p.kind === "action") return run(key);
      if (p.kind === "coder") return coder(key);
      if (p.state !== "pending") return;
      const mine = deps.current();
      Proposals.settle(deps.chat(), key, { state: "sending", feedback: "" });
      deps.draw();
      const answer = await deps.ask(
        p.kind === "vault"
          ? { type: "vault_save_proposal", profile: deps.profile(), tab: tabOf(deps.page && deps.page()),
              proposal: { id: p.id, kind: p.kind, url: p.url, selection: p.selection, name: p.name, nonce: p.nonce } }
          : { type: "task_add_proposal", profile: deps.profile(), proposal: { id: p.id, kind: p.kind, title: p.title, notes: p.notes, nonce: p.nonce } }
      );
      await settle(key, Proposals.afterAdd(answer), mine);
    }

    // Start recording, or Record again: the click is the only thing that starts one.
    // The goal is the field's, cleaned; the page is the card's own.
    async function startRecording(p, value) {
      if (!["pending", "empty", "refused"].includes(p.state) || !deps.record) return;
      const key = keyOf(p);
      const goal = Proposals.cleanGoal(value !== undefined ? value : p.goal);
      if (!goal) return settle(key, { feedback: "Write what you will do first, then press Start recording" });
      const moved = movedFrom(p.page);
      if (moved) return settle(key, { feedback: moved });
      const mine = deps.current();
      Proposals.settle(deps.chat(), key, { state: "sending", feedback: "", goal });
      deps.draw();
      try {
        const st = await deps.record.start(goal, p.page ? p.page.tabId : undefined);
        await settle(key, { state: "recording", recordingId: st && st.id, feedback: "" }, mine);
      } catch (err) {
        await settle(key, { state: "refused", feedback: `Not started: ${errorOf(err)}` }, mine);
      }
    }

    /** movedFrom says why a card proposed on `bound` cannot record the page the panel shows now; "" when it can. */
    function movedFrom(bound, button = "Start recording", sameOrigin = false) {
      if (!bound) return "";
      const now = deps.page && deps.page();
      const origin = (u) => {
        try {
          const x = new URL(u);
          return x.protocol === "file:" ? x.href.split("#")[0] : x.origin; // a file has no origin: the file itself
        } catch {
          return String(u || "");
        }
      };
      // A recording is of one address; an action or a handoff only needs the same site (a page that navigates within itself is still the page).
      const bare = sameOrigin ? origin : (u) => String(u || "").split("#")[0];
      if (now && now.tabId === bound.tabId && bare(now.url) === bare(bound.url)) return "";
      const short = (u) => {
        try {
          const x = new URL(u);
          return x.host + x.pathname;
        } catch {
          return String(u || "").slice(0, 80);
        }
      };
      return `This was suggested for ${short(bound.url)}, but the panel shows ${now ? short(now.url) : "no page"} now. Go back to that page, then press ${button} again.`;
    }

    // What a finished Run says: done, or why not (the worker's words).
    function afterRun(answer, action) {
      const a = answer || {};
      if (a.ok) {
        const did = { click: "clicked", double_click: "double-clicked", type: "typed into" }[action];
        // What was actually matched, so a partial match is never passed off as the words on the card.
        const name = String(a.matched || "").replace(/[\u0000-\u001f\u007f-\u009f\u200b-\u200f\u202a-\u202e\u2066-\u2069]/g, " ").trim().slice(0, 200);
        const clean = (v) => String(v || "").replace(/[\u0000-\u001f\u007f-\u009f\u200b-\u200f\u202a-\u202e\u2066-\u2069]/g, " ").trim().slice(0, 120);
        // A step in a frame is the frame's page, not the one on the tab's address: say which.
        const where = a.frame ? ` in an iframe${clean(a.frameOrigin) ? ` (${clean(a.frameOrigin)})` : ""}` : "";
        const text = action === "open_url" ? "Opened in a new tab." : did && name ? `Done: ${did} \u201c${name}\u201d${where}` : `Done${where}.`;
        return { state: "done", feedback: text, code: null };
      }
      // No answer, or the call itself threw: it may have happened. Never offered again.
      if (a.code === "frame_confirm") {
        const o = String(a.frameOrigin || "").replace(/[\u0000-\u001f\u007f-\u009f\u200b-\u200f\u202a-\u202e\u2066-\u2069]/g, " ").trim().slice(0, 120);
        return { state: "failed", feedback: `Not done yet: that field is in an iframe (${o}), not the page itself. Press Run again to use it.`, code: "frame_confirm", frameOrigin: o };
      }
      if (!answer || a.code === "unknown_outcome" || a.code === "call_failed") return { state: "unknown", feedback: Proposals.UNKNOWN, code: null };
      return { state: "failed", feedback: `Not done: ${a.error || "it failed"}`, code: a.code || "failed" };
    }

    // Run: the click is the only thing that does anything, and only on the page the card was made for.
    async function run(key) {
      const p = find(key);
      if (!p || p.kind !== "action" || !["pending", "failed"].includes(p.state)) return;
      if (p.state === "failed" && p.code === "submit_blocked") return;
      if (p.action !== "open_url") {
        if (!p.page) return settle(key, { feedback: "This was not suggested for a page: ask again with the page open." });
        const moved = movedFrom(p.page, "Run", true);
        if (moved) return settle(key, { feedback: moved });
      }
      const mine = deps.current();
      const frameOrigin = p.code === "frame_confirm" && p.frameOrigin ? p.frameOrigin : "";
      Proposals.settle(deps.chat(), key, { state: "sending", feedback: "", code: null });
      deps.draw();
      let answer;
      try {
        answer = await deps.ask({
          type: "page_action_run",
          tab: tabOf(p.page),
          ...(frameOrigin ? { frameOrigin } : {}),
          proposal: { id: p.id, kind: p.kind, action: p.action, target: p.target, text: p.text, url: p.url, nonce: p.nonce },
        });
      } catch (err) {
        answer = { ok: false, error: errorOf(err), code: "call_failed" };
      }
      await settle(key, afterRun(answer, p.action), mine);
      // The page changed under the step: the next message reads it again.
      if (answer && answer.ok && p.action !== "open_url" && deps.acted) deps.acted();
    }

    // Hand to coder: a coder chat is queued for this tab with the card's words only.
    async function coder(key) {
      const p = find(key);
      const handoff = p && p.kind === "coder" && p.state === "pending";
      const retry = p && p.kind === "action" && ["failed", "unknown"].includes(p.state) && p.code !== "submit_blocked";
      if (!handoff && !retry) return;
      // The tab is the card's own page: a card with none does not borrow the one on screen.
      if (!p.page) return settle(key, { feedback: "This was not suggested for a page: ask again with the page open." });
      const now = deps.page && deps.page();
      if (!now || !now.tabId) return settle(key, { feedback: "There is no page to hand over." });
      const moved = movedFrom(p.page, "Hand to coder", true);
      if (moved) return settle(key, { feedback: moved });
      const v = Proposals.view(p);
      const note = (handoff ? [p.goal, p.why] : [`${v.title}.`, p.action === "type" ? p.text : ""]).filter(Boolean).join("\n\n");
      const mine = deps.current();
      Proposals.settle(deps.chat(), key, { state: "sending", feedback: "" });
      deps.draw();
      let answer;
      try {
        answer = await deps.ask({ type: "coder_queue", kind: "ask", tabId: now.tabId, note, profile: deps.profile() });
      } catch (err) {
        answer = { ok: false, error: errorOf(err) };
      }
      const queued = !!(answer && answer.ok);
      await settle(
        key,
        queued
          ? { state: "queued", feedback: Proposals.QUEUED }
          : { state: handoff ? "pending" : "failed", feedback: `Not queued: ${(answer && answer.error) || "no answer from the extension"}` },
        mine
      );
    }

    async function dismiss(key) {
      const p = find(key);
      const open = p && p.kind === "recording" ? ["pending", "empty", "refused"] : p && p.kind === "action" ? ["pending", "failed", "unknown"] : ["pending"];
      if (!p || !open.includes(p.state)) return;
      await settle(key, { state: "dismissed" });
    }

    // Review and save: the analysis of the card's recording, drawn as a draft
    // in the Record section for the person to check and save there.
    async function review(key) {
      const p = find(key);
      if (!p || p.kind !== "recording" || p.state !== "recorded" || !deps.record) return;
      const mine = deps.current();
      await settle(key, { state: "reviewing", feedback: "" });
      try {
        await deps.record.analyze(p.recordingId);
        await settle(key, { state: "reviewing", feedback: "The draft is in the Record section: check it, then press Save there." }, mine);
      } catch (err) {
        await settle(key, { state: "recorded", feedback: `Analysis failed: ${errorOf(err)}` }, mine);
      }
    }

    /**
     * follow moves the recording cards along with the Record section: its
     * state ({type:"state", state}) and its saves ({type:"saved",
     * recordingId, text}). A card follows its own recording only.
     */
    function follow(event) {
      let changed = false;
      const patch = (p, next) => {
        if (p.state === next.state && p.feedback === next.feedback) return;
        Proposals.settle(deps.chat(), keyOf(p), next);
        changed = true;
      };
      for (const p of cardsOf()) {
        if (p.kind !== "recording" || !p.recordingId) continue;
        if (event && event.type === "saved") {
          if (event.recordingId === p.recordingId && (p.state === "recorded" || p.state === "reviewing")) {
            patch(p, { state: "saved", feedback: String(event.text || "Saved.") });
          }
          continue;
        }
        const s = event && event.type === "state" ? event.state : null;
        // An empty Record section (not restored yet, or cleared) says nothing.
        if (!s || !s.id || p.state !== "recording" || s.recording) continue;
        if (s.id !== p.recordingId) {
          patch(p, { state: "empty", feedback: "This recording is no longer in the Record section." });
        } else if (s.discarded) {
          patch(p, { state: "empty", feedback: "Nothing was recorded." });
        } else if (s.delivered) {
          const n = (s.steps || []).length;
          patch(p, { state: "recorded", feedback: `Recorded ${n} ${n === 1 ? "step" : "steps"}.` });
        } else {
          patch(p, { state: "recording", feedback: "Stopped. Waiting for MonoAgent to receive the recording…" });
        }
      }
      if (changed) {
        deps.save();
        deps.draw();
      }
    }

    return { confirm, dismiss, review, run, coder, follow };
  }

  root.MonoChatCards = { create };
})(globalThis);
