/**
 * MonoAgent Bridge — the coder's right-click items (C6)
 *
 * "Send this element to coder", "Report a bug on this page to coder" and
 * "Extract design tokens to coder", in the MonoAgent menu after the
 * capture and task items. Each opens the side panel's Coder section filled
 * in (MonoCoderBridge.handoff): the kind, the tab, and for the element, the
 * selector of the element that was right-clicked. highlight_panel.js
 * remembers that element (a content-script contextmenu listener, real
 * clicks only), and the worker reads it back with the recorder's selector
 * ranking. Nothing is queued from here: the person reads the form and
 * presses Queue.
 *
 * The items hang under capture_modes.js's "MonoAgent" parent, which
 * capture_bridge.js's registrar empties (removeAll) and refills in
 * removeAll's callback on every worker start. Created straight away they
 * would be created before their parent exists and fail. So they are created
 * in the callback of a call made after that removeAll: Chrome answers a
 * worker's calls in order, so that callback runs after the refill was
 * issued, and the parent exists by the time these reach it.
 */

(function (root) {
  "use strict";

  const IDS = {
    separator: "monoagent-coder-separator",
    element: "monoagent-coder-element",
    bug: "monoagent-coder-bug",
    tokens: "monoagent-coder-tokens",
  };
  const KIND_OF = { [IDS.bug]: "bug-report", [IDS.tokens]: "tokens" };

  let installed = false;

  const parentId = () => (root.MonoCaptureModes && root.MonoCaptureModes.ROOT_ID) || "monoagent-root";

  /** menuItems is every contextMenus.create call, in order. Only where the parent shows. */
  function menuItems() {
    const parent = parentId();
    return [
      { id: IDS.separator, parentId: parent, type: "separator", contexts: ["page", "selection", "video"] },
      { id: IDS.element, parentId: parent, title: "Send this element to coder", contexts: ["page", "selection", "video"] },
      { id: IDS.bug, parentId: parent, title: "Report a bug on this page to coder", contexts: ["page"] },
      { id: IDS.tokens, parentId: parent, title: "Extract design tokens to coder", contexts: ["page"] },
    ];
  }

  function register() {
    const menus = chrome.contextMenus;
    if (!menus || !menus.create) return;
    const items = menuItems();
    const createAll = () => {
      void chrome.runtime.lastError; // the parent is not back yet when update runs: expected
      for (const item of items) menus.create(item, () => void chrome.runtime.lastError);
    };
    // A no-op on the parent, for its callback's place in the queue (above).
    if (menus.update) menus.update(parentId(), {}, createAll);
    else createAll();
  }

  /** install registers the items and their click handler, once per worker. */
  function install() {
    if (installed) return;
    installed = true;
    register();
    if (chrome.contextMenus && chrome.contextMenus.onClicked) {
      chrome.contextMenus.onClicked.addListener((info, tab) => {
        const run = handleMenuClick(info, tab);
        if (run) run.catch((err) => console.warn("[monoagent] coder from the menu:", err.message));
      });
    }
  }

  /**
   * handleMenuClick answers the three coder items: the side panel opens at
   * once (inside the click), then the prefill follows. Resolves the prefill,
   * or returns null for anyone else's item or a click with no tab.
   */
  function handleMenuClick(info, tab) {
    const id = info && info.menuItemId;
    if (!tab || !Number.isInteger(tab.id) || (id !== IDS.element && !KIND_OF[id])) return null;
    const B = root.MonoCoderBridge;
    if (id === IDS.element) return B.handoff(tab, { which: "context", frameId: info.frameId });
    return B.handoff(tab, { kind: KIND_OF[id] });
  }

  root.MonoCoderMenu = { install, handleMenuClick, menuItems, IDS };
})(globalThis);
