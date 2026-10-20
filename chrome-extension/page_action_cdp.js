/**
 * MonoAgent Bridge — a chat page action through the debugger
 *
 * The fallback when chrome.scripting refuses a page (page_action_bridge.js):
 * the same target, found the same way, through the accessibility tree and
 * DOM domains, then done with real input events. Every command goes through
 * MonoAgentCdp.send, background.js's own attach and send, so a tab is never
 * attached twice.
 *
 *   run(tabId, {action, target, text}) -> {ok:true} | {ok:false, code, error}
 *
 * Matching mirrors page_action_page.js: not-ignored, enabled nodes with a box
 * on screen, exact accessible name first, then a name that contains the words;
 * the innermost of nested matches; several equally good ones are never
 * guessed between. The same guard() runs on the element (callFunctionOn)
 * before anything is clicked.
 *
 * After agent_cdp.js and page_action_page.js. Node-tested with a fake CDP
 * (page_action_cdp.test.mjs).
 */

(function (root) {
  "use strict";

  const MAX_CANDIDATES = 40;
  const SKIP_ROLES = new Set(["RootWebArea", "WebArea", "StaticText", "InlineTextBox", "LineBreak", "generic", "none", "presentation", "document"]);
  const FIELD_ROLES = new Set(["textbox", "searchbox", "combobox"]);

  const norm = (s) => String(s == null ? "" : s).replace(/\s+/g, " ").trim().toLowerCase();
  const val = (v) => (v && v.value !== undefined && v.value !== null ? v.value : "");
  const gone = (reason) => ({ ok: false, code: reason.code, error: reason.error });

  /** candidates: the AX nodes that could be `target`, by tier (exact name, name that contains the words). */
  function candidates(axNodes, target, action) {
    const want = norm(target);
    const longest = Math.max(want.length * 3, want.length + 60);
    const out = { byId: new Map(axNodes.map((n) => [n.nodeId, n])), exact: [], partial: [] };
    for (const n of axNodes) {
      const role = String(val(n.role));
      if (n.ignored || SKIP_ROLES.has(role) || !Number.isInteger(n.backendDOMNodeId)) continue;
      if (action === "type" && !FIELD_ROLES.has(role)) continue;
      if ((n.properties || []).some((p) => p.name === "disabled" && val(p.value) === true)) continue;
      const name = norm(val(n.name));
      if (!name) continue;
      if (name === want) out.exact.push(n);
      else if (name.includes(want) && name.length <= longest) out.partial.push(n);
    }
    return out;
  }

  /** pick: of the candidates on screen (shown), the best tier's innermost nodes. */
  function pick(c, shown) {
    const exact = c.exact.filter((n) => shown.has(n.nodeId));
    const tier = exact.length ? exact : c.partial.filter((n) => shown.has(n.nodeId));
    // Whatever has a match inside it is not itself the answer.
    const outer = new Set();
    for (const n of tier) {
      for (let up = c.byId.get(n.parentId); up; up = c.byId.get(up.parentId)) outer.add(up.nodeId);
    }
    return tier.filter((n) => !outer.has(n.nodeId));
  }

  const area = (q) => {
    // A quad is four corners: x1 y1 x2 y2 x3 y3 x4 y4.
    let a = 0;
    for (let i = 0; i < 4; i++) {
      const j = (i + 1) % 4;
      a += q[i * 2] * q[j * 2 + 1] - q[j * 2] * q[i * 2 + 1];
    }
    return Math.abs(a) / 2;
  };

  async function centre(send, backendNodeId) {
    try {
      const { quads } = await send("DOM.getContentQuads", { backendNodeId });
      const q = (quads || []).find((x) => x.length === 8 && area(x) > 0);
      if (!q) return null;
      return { x: (q[0] + q[2] + q[4] + q[6]) / 4, y: (q[1] + q[3] + q[5] + q[7]) / 4 };
    } catch {
      return null; // no box: not on screen
    }
  }

  async function guardOf(send, backendNodeId, action) {
    const { object } = await send("DOM.resolveNode", { backendNodeId });
    try {
      const res = await send("Runtime.callFunctionOn", {
        objectId: object.objectId,
        functionDeclaration: root.MonoPageAction.guard.toString(),
        arguments: [{ value: action }],
        returnByValue: true,
      });
      return String((res && res.result && res.result.value) || "");
    } finally {
      send("Runtime.releaseObject", { objectId: object.objectId }).catch(() => {});
    }
  }

  async function press(send, at, count) {
    await send("Input.dispatchMouseEvent", { type: "mouseMoved", x: at.x, y: at.y });
    for (let n = 1; n <= count; n++) {
      await send("Input.dispatchMouseEvent", { type: "mousePressed", x: at.x, y: at.y, button: "left", buttons: 1, clickCount: n });
      await send("Input.dispatchMouseEvent", { type: "mouseReleased", x: at.x, y: at.y, button: "left", buttons: 0, clickCount: n });
    }
  }

  async function typeInto(send, backendNodeId, text) {
    await send("DOM.focus", { backendNodeId });
    const { object } = await send("DOM.resolveNode", { backendNodeId });
    try {
      // What is in the field is replaced, as typing over a selection does.
      await send("Runtime.callFunctionOn", {
        objectId: object.objectId,
        functionDeclaration: "function(){this.focus();if(typeof this.select==='function')this.select();else getSelection().selectAllChildren(this);}",
      });
    } finally {
      send("Runtime.releaseObject", { objectId: object.objectId }).catch(() => {});
    }
    await send("Input.insertText", { text });
  }

  async function run(tabId, act) {
    const send = (method, params) => root.MonoAgentCdp.send(tabId, method, params || {});
    const { nodes } = await send("Accessibility.getFullAXTree", {});
    const c = candidates(nodes || [], act.target, act.action);
    const shown = new Set();
    for (const n of [...c.exact, ...c.partial].slice(0, MAX_CANDIDATES)) {
      if (await centre(send, n.backendDOMNodeId)) shown.add(n.nodeId);
    }
    const found = pick(c, shown);
    if (found.length === 0) return gone({ code: "not_found", error: "not found" });
    if (found.length > 1) return gone({ code: "ambiguous", error: `ambiguous (${found.length} matches)` });
    const id = found[0].backendDOMNodeId;
    const blocked = await guardOf(send, id, act.action);
    if (blocked === "submit_blocked") return gone({ code: blocked, error: "that would submit a form, so it was not clicked: press that button yourself" });
    if (blocked) return gone({ code: blocked, error: "that field is not one to type into" });
    await send("DOM.scrollIntoViewIfNeeded", { backendNodeId: id });
    if (act.action === "type") {
      await typeInto(send, id, String(act.text || ""));
      return { ok: true };
    }
    const at = await centre(send, id); // it moved when it scrolled
    if (!at) return gone({ code: "not_found", error: "not found" });
    await press(send, at, act.action === "double_click" ? 2 : 1);
    return { ok: true };
  }

  root.MonoPageActionCdp = { run, candidates, pick };
})(globalThis);
