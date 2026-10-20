// The debugger fallback of a chat page action (page_action_cdp.js): the same
// target found through the accessibility and DOM domains, scrolled into view,
// then real input events. A fake CDP stands behind agent_cdp.js's send.
// `CHROME_PATH=/nonexistent node --test chrome-extension/page_action_cdp.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const QUAD = (x, y, w = 100, h = 20) => [x, y, x + w, y, x + w, y + h, x, y + h];

// A page as Chrome's accessibility tree describes it; guards: backendId -> what guard() answers.
function world({ nodes, quads = {}, guards = {} } = {}) {
  const calls = [];
  const g = loadExtensionScripts(["agent_cdp.js", "page_action_page.js", "page_action_cdp.js"], {});
  g.MonoAgentCdp.install({
    attach: async (tabId) => calls.push(["attach", tabId]),
    cdp: async (tabId, method, params) => {
      calls.push([method, params]);
      switch (method) {
        case "Accessibility.getFullAXTree": return { nodes };
        case "DOM.getContentQuads":
          if (!quads[params.backendNodeId]) throw new Error("Could not compute content quads.");
          return { quads: [quads[params.backendNodeId]] };
        case "DOM.resolveNode": return { object: { objectId: `obj${params.backendNodeId}` } };
        case "Runtime.callFunctionOn": {
          const id = Number(String(params.objectId).slice(3));
          return { result: { value: params.arguments ? guards[id] || "" : undefined } };
        }
        default: return {};
      }
    },
    onDebuggerEvent: () => {},
    onDebuggerDetach: () => {},
  });
  const methods = () => calls.map((c) => c[0]);
  return { cdp: g.MonoPageActionCdp, calls, methods, sent: (m) => calls.filter((c) => c[0] === m).map((c) => c[1]) };
}

const ax = (nodeId, role, name, extra = {}) => Object.assign({ nodeId: String(nodeId), role: { value: role }, name: { value: name }, backendDOMNodeId: nodeId }, extra);
const TREE = [
  ax(1, "RootWebArea", "Drive", { childIds: ["2", "5", "6", "7"] }),
  ax(2, "option", "CV - Morteza Nokhodian 2025 Apr", { parentId: "1", childIds: ["3"] }),
  ax(3, "gridcell", "CV - Morteza Nokhodian 2025 Apr", { parentId: "2", childIds: ["4"] }),
  ax(4, "StaticText", "CV - Morteza Nokhodian 2025 Apr", { parentId: "3" }),
  ax(5, "button", "Share", { parentId: "1" }),
  ax(6, "button", "Share with everyone", { parentId: "1" }),
  ax(7, "textbox", "Search in Drive", { parentId: "1" }),
  ax(8, "button", "Hidden thing", { parentId: "1", ignored: true }),
];
const QUADS = { 2: QUAD(10, 100), 3: QUAD(10, 100), 5: QUAD(300, 10, 50, 20), 6: QUAD(400, 10, 50, 20), 7: QUAD(0, 0, 200, 30) };

test("a double-click scrolls the item into view and presses twice: clickCount 1 then 2, at its centre", async () => {
  const w = world({ nodes: TREE, quads: QUADS });
  assert.deepEqual(await w.cdp.run(4, { action: "double_click", target: "cv - morteza nokhodian 2025 apr" }), { ok: true });
  assert.deepEqual(w.sent("DOM.scrollIntoViewIfNeeded"), [{ backendNodeId: 3 }], "the innermost of the nested matches, not the text node");
  const mouse = w.sent("Input.dispatchMouseEvent").map((m) => [m.type, m.clickCount || "-", m.x, m.y].join(":"));
  assert.deepEqual(mouse, ["mouseMoved:-:60:110", "mousePressed:1:60:110", "mouseReleased:1:60:110", "mousePressed:2:60:110", "mouseReleased:2:60:110"]);
  assert.ok(w.methods().indexOf("DOM.scrollIntoViewIfNeeded") < w.methods().indexOf("Input.dispatchMouseEvent"));
  assert.deepEqual(w.calls[0], ["attach", 4], "through background.js's own attach");
});

test("a click is one press; an exact name beats a longer one", async () => {
  const w = world({ nodes: TREE, quads: QUADS });
  assert.deepEqual(await w.cdp.run(4, { action: "click", target: "Share" }), { ok: true });
  assert.deepEqual(w.sent("Input.dispatchMouseEvent").filter((m) => m.type === "mousePressed").map((m) => [m.clickCount, m.x]), [[1, 325]]);
});

test("not found, ignored nodes, nodes with no box, and ambiguity are answers; nothing is pressed", async () => {
  const w = world({ nodes: TREE, quads: Object.assign({}, QUADS, { 5: undefined }) });
  const r = (target, action = "click") => w.cdp.run(4, { action, target });
  assert.deepEqual(await r("Nothing like it"), { ok: false, code: "not_found", error: "not found" });
  assert.deepEqual(await r("Hidden thing"), { ok: false, code: "not_found", error: "not found" });
  assert.deepEqual(await r("Share"), { ok: true }, "the exact 'Share' has no box on screen, so it is out: the one that contains the words is the one left");
  const two = world({ nodes: TREE.concat([ax(9, "button", "Share with everyone", { parentId: "1" })]), quads: Object.assign({}, QUADS, { 9: QUAD(500, 10) }) });
  assert.deepEqual(await two.cdp.run(4, { action: "click", target: "with everyone" }), { ok: false, code: "ambiguous", error: "ambiguous (2 matches)" });
  assert.deepEqual(two.sent("Input.dispatchMouseEvent"), [], "no guess");
  assert.deepEqual((await r("Search in Drive", "type").then((o) => o.ok)), true, "a textbox is a field");
  assert.deepEqual(await r("Share", "type"), { ok: false, code: "not_found", error: "not found" }, "a button is not a field");
});

test("a click the page guard calls a submit is not made", async () => {
  const w = world({ nodes: TREE, quads: QUADS, guards: { 5: "submit_blocked" } });
  const out = await w.cdp.run(4, { action: "click", target: "Share" });
  assert.deepEqual([out.ok, out.code], [false, "submit_blocked"]);
  assert.match(out.error, /press that button yourself/);
  assert.deepEqual(w.sent("Input.dispatchMouseEvent"), []);
  assert.match(w.sent("Runtime.callFunctionOn")[0].functionDeclaration, /submit_blocked/, "the same guard as the page half, run on the element");
});

test("typing focuses the field, selects what is in it, and inserts the text", async () => {
  const w = world({ nodes: TREE, quads: QUADS });
  assert.deepEqual(await w.cdp.run(4, { action: "type", target: "Search in Drive", text: "annual report" }), { ok: true });
  assert.deepEqual(w.sent("DOM.focus"), [{ backendNodeId: 7 }]);
  assert.deepEqual(w.sent("Input.insertText"), [{ text: "annual report" }]);
  assert.deepEqual(w.sent("Input.dispatchMouseEvent"), []);
  const blocked = world({ nodes: TREE, quads: QUADS, guards: { 7: "not_allowed" } });
  const out = await blocked.cdp.run(4, { action: "type", target: "Search in Drive", text: "x" });
  assert.deepEqual([out.ok, out.code, blocked.sent("Input.insertText")], [false, "not_allowed", []]);
});
