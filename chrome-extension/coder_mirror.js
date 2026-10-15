/**
 * MonoAgent Bridge — a coder conversation, mirrored in the side panel (C6)
 *
 * The Coder section (sidepanel_coder.js) polls `coder.events` and draws one
 * conversation as it happens. The records are chat.events records, so they
 * fold into chat_core.js's transcript state as the chat's do; a record may
 * also carry `images: [{path, dataUrl}]`, the screenshots its tool wrote in
 * the project folder, which are kept under the reply they belong to.
 *
 * SAFETY, as in chat_render.js: the reply may say whatever a page talked the
 * coder into, so the DOM is built only with createElement, textContent and
 * attributes: never innerHTML. An image is shown only from a base64 data URL
 * of a raster type, never from a path or a remote address.
 *
 *   watchState(id)              a fresh mirror for one conversation
 *   fold(state, events)         -> changed?   pure, node-tested
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
    return { id, chat: Core().newState(null), shots: [], since: 0, running: false };
  }

  function lastAssistant(chat) {
    for (let i = chat.messages.length - 1; i >= 0; i--) if (chat.messages[i].role === "assistant") return chat.messages[i];
    return null;
  }

  /** fold applies events and keeps their screenshots, under the reply they belong to. */
  function fold(state, events) {
    let changed = false;
    for (const e of Array.isArray(events) ? events : []) {
      if (!e || typeof e.type !== "string" || !Number.isFinite(e.seq)) continue;
      const ev = { seq: e.seq, type: e.type, payload: e.payload && typeof e.payload === "object" ? e.payload : {} };
      if (e.turn !== undefined && e.turn !== null && e.turn !== "") ev.turn = String(e.turn);
      if (Core().applyEvent(state.chat, ev)) changed = true;
      const images = Array.isArray(e.images) ? e.images : [];
      const owner = images.length ? lastAssistant(state.chat) : null;
      for (const img of images) {
        const url = img && typeof img.dataUrl === "string" ? img.dataUrl : "";
        if (!owner || url.length > IMAGE_CAP || !IMAGE.test(url)) continue;
        state.shots.push({ owner: owner.id, path: String(img.path || ""), url });
        changed = true;
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
   * followable: MonoAgent says this conversation takes a message from here.
   * It answers `canSend` itself; a bridge that does not falls back to the
   * rule it enforces (decision 3): the app started its first turn, and no
   * turn is running.
   */
  function followable(state, conv) {
    if (!state || state.running || !conv) return false;
    return typeof conv.canSend === "boolean" ? conv.canSend : !!conv.startedInDesktop && !conv.running;
  }

  root.MonoCoderMirror = { watchState, fold, render, followable, baseName, IMAGE_CAP };
})(globalThis);
