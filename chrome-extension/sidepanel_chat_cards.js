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
 *
 *   create(deps) -> { confirm(key), dismiss(key), review(key), follow(event) }
 *
 * A card is named by its key (MonoChatProposals.keyOf), not its call id:
 * two turns' cards can share a call id.
 *
 * deps: chat() the conversation on screen, ask(message) the worker, profile(),
 * current() a token that changes when the panel switches profile, save(),
 * draw(), record (MonoPanelRecord, or null).
 */

(function (root) {
  "use strict";

  const Proposals = root.MonoChatProposals;

  function create(deps) {
    const cardsOf = () => (deps.chat().messages || []).flatMap((m) => m.proposals || []);
    const keyOf = Proposals.keyOf;
    const find = (key) => cardsOf().find((p) => keyOf(p) === key) || null;
    const errorOf = (err) => (err && err.message) || String(err);

    async function settle(key, patch, mine) {
      if (mine !== undefined && mine !== deps.current()) return; // another profile's chat is on screen now
      Proposals.settle(deps.chat(), key, patch);
      await deps.save();
      deps.draw();
    }

    async function confirm(key) {
      const p = find(key);
      if (!p) return;
      if (p.kind === "recording") return startRecording(p);
      if (p.state !== "pending") return;
      const mine = deps.current();
      Proposals.settle(deps.chat(), key, { state: "sending", feedback: "" });
      deps.draw();
      const answer = await deps.ask({ type: "task_add_proposal", profile: deps.profile(), proposal: { id: p.id, kind: p.kind, title: p.title, notes: p.notes, nonce: p.nonce } });
      await settle(key, Proposals.afterAdd(answer), mine);
    }

    // Start recording, or Record again: the click is the only thing that starts one.
    async function startRecording(p) {
      if (!["pending", "empty", "refused"].includes(p.state) || !deps.record) return;
      const mine = deps.current();
      const key = keyOf(p);
      Proposals.settle(deps.chat(), key, { state: "sending", feedback: "" });
      deps.draw();
      try {
        const st = await deps.record.start(p.goal);
        await settle(key, { state: "recording", recordingId: st && st.id, feedback: "" }, mine);
      } catch (err) {
        await settle(key, { state: "refused", feedback: `Not started: ${errorOf(err)}` }, mine);
      }
    }

    async function dismiss(key) {
      const p = find(key);
      const open = p && p.kind === "recording" ? ["pending", "empty", "refused"] : ["pending"];
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

    return { confirm, dismiss, review, follow };
  }

  root.MonoChatCards = { create };
})(globalThis);
