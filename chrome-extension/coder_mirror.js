/**
 * MonoAgent Bridge — a coder conversation, mirrored in the side panel (C6)
 *
 * The Coder section (sidepanel_coder.js) polls `coder.events` and draws one
 * conversation as it happens. The records are chat.events records, so they
 * fold into chat_core.js's transcript state as the chat's do; the reply also
 * carries `images: [{seq, path, dataUrl}]`, the screenshots a tool result
 * named in the project folder, which are kept under the reply of that event.
 *
 * SAFETY, as in chat_render.js: the reply may say whatever a page talked the
 * coder into, so the DOM is built only with createElement, textContent and
 * attributes: never innerHTML. An image is shown only from a base64 data URL
 * of a raster type, never from a path or a remote address.
 *
 *   watchState(id)              a fresh mirror for one conversation
 *   fold(state, reply)          -> changed?   a coder.events reply; pure, node-tested
 *   render(doc, state)          -> DocumentFragment of the transcript
 *   followable(state, conv)     may the follow-up box show? (coder.send)
 *
 * After chat_core.js and chat_render.js.
 */

(function (root) {
  "use strict";

  const Core = () => root.MonoChatCore;
  const Render = () => root.MonoChatRender;

  // The bridge sends a screenshot only up to 2 MB; base64 adds a third.
  const IMAGE = /^data:image\/(png|jpeg|webp|gif);base64,[A-Za-z0-9+/]+=*$/;
  const IMAGE_CAP = 3 * 1024 * 1024;
  // The first message is the queued job's prompt: long, and the person wrote little of it.
  const USER_TEXT_CAP = 600;

  const baseName = (p) => String(p || "").split(/[\\/]/).filter(Boolean).pop() || String(p || "");

  function watchState(id) {
    return { id, chat: Core().newState(null), shots: [], running: false };
  }

  function lastAssistant(chat) {
    for (let i = chat.messages.length - 1; i >= 0; i--) if (chat.messages[i].role === "assistant") return chat.messages[i];
    return null;
  }

  /**
   * fold applies one coder.events reply: its events, in order, stamped with
   * its turn (seq counts per turn), and each screenshot under the reply its
   * event belongs to. An event already seen changes nothing, and nor does
   * its screenshot.
   */
  function fold(state, reply) {
    const r = reply || {};
    const images = Array.isArray(r.images) ? r.images : [];
    let changed = false;
    for (const e of Core().normalizeEvents(r.events, r.turn)) {
      if (!Core().applyEvent(state.chat, e)) continue;
      changed = true;
      const owner = lastAssistant(state.chat);
      for (const img of images) {
        const url = img && img.seq === e.seq && typeof img.dataUrl === "string" ? img.dataUrl : "";
        if (!owner || !url || url.length > IMAGE_CAP || !IMAGE.test(url)) continue;
        state.shots.push({ owner: owner.id, path: String(img.path || ""), url });
      }
    }
    return changed;
  }

  function message(doc, state, m) {
    const box = doc.createElement("div");
    box.className = "chat-msg";
    box.dataset.role = m.role;
    if (m.error) box.dataset.error = "true";
    if (m.role === "assistant") {
      if (m.tools.length) {
        const cards = doc.createElement("div");
        cards.className = "chat-tool-row";
        for (const t of m.tools) {
          const card = doc.createElement("span");
          card.className = "chat-tool";
          card.dataset.status = t.status;
          card.textContent = t.status === "failed" ? `${t.name} failed` : t.name;
          cards.appendChild(card);
        }
        box.appendChild(cards);
      }
      if (m.text) box.appendChild(Render().toDom(doc, Render().parse(m.text)));
      for (const s of state.shots.filter((x) => x.owner === m.id)) {
        const img = doc.createElement("img");
        img.className = "coder-shot";
        img.alt = `Screenshot ${baseName(s.path)}`;
        img.title = s.path;
        img.src = s.url;
        box.appendChild(img);
      }
    } else if (m.role === "user") {
      const text = doc.createElement("div");
      text.className = "chat-text";
      text.textContent = m.text.length > USER_TEXT_CAP ? `${m.text.slice(0, USER_TEXT_CAP)}…` : m.text;
      box.appendChild(text);
    } else {
      box.textContent = m.text;
    }
    return box;
  }

  /** render builds the whole transcript; an empty one says so. */
  function render(doc, state) {
    const frag = doc.createDocumentFragment();
    for (const m of state.chat.messages) frag.appendChild(message(doc, state, m));
    if (!state.chat.messages.length) {
      const empty = doc.createElement("p");
      empty.className = "chat-empty";
      empty.textContent = "Nothing has happened in this conversation yet.";
      frag.appendChild(empty);
    }
    return frag;
  }

  /**
   * followable: would MonoAgent take a message from here (coder.send)?
   * Decision 3's rule, which the bridge enforces: the app ran a turn of this
   * conversation to its end (`startedInDesktop`, from coder.list), and no
   * turn is running now.
   */
  function followable(state, conv) {
    return !!(state && conv && !state.running && conv.startedInDesktop === true && !conv.running);
  }

  root.MonoCoderMirror = { watchState, fold, render, followable, baseName, IMAGE_CAP };
})(globalThis);
