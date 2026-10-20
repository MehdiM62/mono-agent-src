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

  /** candidates: the AX nodes that could be `target`, by tier (exact name; whole-word containment in a name of at most twice the length, never for typing). */
  function candidates(axNodes, target, action) {
    const want = norm(target);
    const out = { byId: new Map(axNodes.map((n) => [n.nodeId, n])), exact: [], partial: [] };
    for (const n of axNodes) {
      const role = String(val(n.role));
      if (n.ignored || SKIP_ROLES.has(role) || !Number.isInteger(n.backendDOMNodeId)) continue;
      if (action === "type" && !FIELD_ROLES.has(role)) continue;
      if ((n.properties || []).some((p) => p.name === "disabled" && val(p.value) === true)) continue;
      const name = norm(val(n.name));
      if (!name) continue;
      if (name === want) out.exact.push(n);
      else if (action !== "type" && name.length <= want.length * 2 && root.MonoPageAction.bounded(name, want)) out.partial.push(n);
    }
    return out;
  }

  /** pick: of the candidates on screen (shown), the best tier's innermost nodes: {nodes, exact}. */
  function pick(c, shown) {
    const exact = c.exact.filter((n) => shown.has(n.nodeId));
    const tier = exact.length ? exact : c.partial.filter((n) => shown.has(n.nodeId));
    // Whatever has a match inside it is not itself the answer.
    const outer = new Set();
    for (const n of tier) {
      for (let up = c.byId.get(n.parentId); up; up = c.byId.get(up.parentId)) outer.add(up.nodeId);
    }
    return { nodes: tier.filter((n) => !outer.has(n.nodeId)), exact: exact.length > 0 };
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

  /** hits: the element at that point is the target or inside it, so the press lands on what was found. */
  async function hits(send, targetId, at) {
    let hit;
    try {
      ({ backendNodeId: hit } = await send("DOM.getNodeForLocation", { x: Math.round(at.x), y: Math.round(at.y) }));
    } catch {
      return false;
    }
    if (hit === targetId) return true;
    if (!Number.isInteger(hit)) return false;
    const a = await send("DOM.resolveNode", { backendNodeId: targetId });
    const b = await send("DOM.resolveNode", { backendNodeId: hit });
    try {
      const res = await send("Runtime.callFunctionOn", {
        objectId: a.object.objectId,
        functionDeclaration: "function(o){return this===o||this.contains(o)}",
        arguments: [{ objectId: b.object.objectId }],
        returnByValue: true,
      });
      return !!(res && res.result && res.result.value === true);
    } finally {
      send("Runtime.releaseObject", { objectId: a.object.objectId }).catch(() => {});
      send("Runtime.releaseObject", { objectId: b.object.objectId }).catch(() => {});
    }
  }

  /** onOrigin: the page is still on the origin the card was proposed on, asked right now. */
  async function onOrigin(send, origin) {
    if (typeof origin !== "string") return true;
    const res = await send("Runtime.evaluate", { expression: "location.origin", returnByValue: true });
    return !!(res && res.result && res.result.value === origin);
  }

  // Focus is checked in the page: the node took it, and it is still a field that may be typed into.
  async function typeInto(send, backendNodeId, text) {
    await send("DOM.focus", { backendNodeId });
    const { object } = await send("DOM.resolveNode", { backendNodeId });
    let state;
    try {
      const res = await send("Runtime.callFunctionOn", {
        objectId: object.objectId,
        functionDeclaration:
          "function(){const guard=" + root.MonoPageAction.guard.toString() + ";this.focus();const a=document.activeElement;if(a!==this)return 'focus_lost';const g=guard.call(a,'type');if(g)return g;" +
          "if(typeof this.select==='function')this.select();else getSelection().selectAllChildren(this);return '';}",
        returnByValue: true,
      });
      state = String((res && res.result && res.result.value) || "");
    } finally {
      send("Runtime.releaseObject", { objectId: object.objectId }).catch(() => {});
    }
    if (state === "focus_lost") return gone({ code: "not_found", error: "the field did not take focus" });
    if (state) return gone({ code: state, error: "that field is not one to type into" });
    await send("Input.insertText", { text });
    return { ok: true };
  }

  async function run(tabId, act) {
    const send = (method, params) => root.MonoAgentCdp.send(tabId, method, params || {});
    const { nodes } = await send("Accessibility.getFullAXTree", {});
    const c = candidates(nodes || [], act.target, act.action);
    const shown = new Set();
    for (const n of [...c.exact, ...c.partial].slice(0, MAX_CANDIDATES)) {
      if (await centre(send, n.backendDOMNodeId)) shown.add(n.nodeId);
    }
    const { nodes: found, exact } = pick(c, shown);
    if (found.length === 0) return gone({ code: "not_found", error: "not found" });
    if (found.length > 1) return gone({ code: "ambiguous", error: `ambiguous (${found.length} matches)` });
    const id = found[0].backendDOMNodeId;
    const matched = String(val(found[0].name)).slice(0, 200);
    const blocked = await guardOf(send, id, act.action);
    if (blocked === "submit_blocked") return gone({ code: blocked, error: root.MonoPageAction.HELD });
    if (blocked) return gone({ code: blocked, error: "that field is not one to type into" });
    await send("DOM.scrollIntoViewIfNeeded", { backendNodeId: id });
    if (!(await onOrigin(send, act.origin))) return gone({ code: "page_changed", error: "the page changed since this was suggested" });
    let started = false;
    try {
      if (act.action === "type") {
        started = true;
        const typed = await typeInto(send, id, String(act.text || ""));
        return typed.ok ? { ok: true, matched, exact } : typed;
      }
      const at = await centre(send, id); // it moved when it scrolled
      if (!at || !(await hits(send, id, at))) return gone({ code: "not_found", error: "not found: something else is on top of it" });
      started = true;
      await press(send, at, act.action === "double_click" ? 2 : 1);
      return { ok: true, matched, exact };
    } catch (err) {
      // Once input was sent, a failure leaves the outcome unknown (the bridge says so).
      if (started) err.started = true;
      throw err;
    }
  }

  root.MonoPageActionCdp = { run, candidates, pick };
})(globalThis);
