// Tests for a11y_tree (agent_a11y.js) on canned Accessibility.getFullAXTree
// answers. `node --test 'chrome-extension/**/*.test.mjs'`.

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const v = (value) => ({ type: "computedString", value });

/**
 * A page: <main><div><h1>Sign up</h1><form><label>Email<input value=a@b.c></label>
 * <button>Create account</button></form></div></main>, as Chrome lists it.
 */
const PAGE = [
  { nodeId: "1", ignored: false, role: v("RootWebArea"), name: v("Sign up · Acme"), childIds: ["2"], backendDOMNodeId: 1 },
  { nodeId: "2", ignored: true, role: v("none"), childIds: ["3"], parentId: "1", backendDOMNodeId: 2 },
  { nodeId: "3", ignored: false, role: v("main"), name: v(""), childIds: ["4"], parentId: "2", backendDOMNodeId: 3 },
  { nodeId: "4", ignored: false, role: v("generic"), name: v(""), childIds: ["5", "7"], parentId: "3", backendDOMNodeId: 4 },
  { nodeId: "5", ignored: false, role: v("heading"), name: v("Sign up"), childIds: ["6"], parentId: "4", backendDOMNodeId: 5 },
  { nodeId: "6", ignored: false, role: v("StaticText"), name: v("Sign up"), childIds: ["6a"], parentId: "5", backendDOMNodeId: 6 },
  { nodeId: "6a", ignored: false, role: v("InlineTextBox"), name: v("Sign up"), childIds: [], parentId: "6" },
  { nodeId: "7", ignored: false, role: v("form"), name: v(""), childIds: ["8", "9"], parentId: "4", backendDOMNodeId: 7 },
  { nodeId: "8", ignored: false, role: v("textbox"), name: v("Email"), value: { type: "string", value: "a@b.c" }, childIds: ["8a"], parentId: "7", backendDOMNodeId: 8 },
  { nodeId: "8a", ignored: false, role: v("StaticText"), name: v("a@b.c"), childIds: [], parentId: "8" },
  { nodeId: "9", ignored: false, role: v("button"), name: v("Create account"), childIds: ["10"], parentId: "7", backendDOMNodeId: 9 },
  { nodeId: "10", ignored: false, role: v("StaticText"), name: v("Create account"), childIds: [], parentId: "9", backendDOMNodeId: 10 },
];

function load(answers = {}) {
  const g = loadExtensionScripts(["agent_cdp.js", "agent_redact.js", "agent_a11y.js"], {});
  const sent = [];
  g.MonoAgentCdp.install({
    attach: async () => {},
    cdp: async (tabId, method, params) => {
      sent.push([method, params]);
      const a = answers[method];
      return typeof a === "function" ? a(params) : a || {};
    },
    onDebuggerEvent: () => {},
    onDebuggerDetach: () => {},
  });
  return { a11y: g.MonoAgentA11y, sent };
}

test("ignored nodes, inline text boxes, unnamed containers and text repeating a name or value are pruned", () => {
  const { a11y } = load();
  assert.deepEqual(a11y.prune(PAGE), {
    nodes: [{
      role: "RootWebArea",
      name: "Sign up · Acme",
      children: [{
        role: "main",
        name: "",
        children: [
          { role: "heading", name: "Sign up" },
          { role: "form", name: "", children: [{ role: "textbox", name: "Email", value: "a@b.c" }, { role: "button", name: "Create account" }] },
        ],
      }],
    }],
  });
});

test("depth counts kept levels only", () => {
  const { a11y } = load();
  assert.deepEqual(a11y.prune(PAGE, { depth: 2 }).nodes[0].children, [{ role: "main", name: "" }]);
});

test("the node cap cuts the tree and says so", () => {
  const { a11y } = load();
  const out = a11y.prune(PAGE, { maxNodes: 3 });
  assert.equal(out.truncated, true);
  assert.equal(JSON.stringify(out).includes("Create account"), false);
  assert.equal("truncated" in a11y.prune(PAGE), false, "only present when it happened");
});

test("a selector roots the tree at its element, through DOM.describeNode", async () => {
  const { a11y, sent } = load({
    "Runtime.evaluate": { result: { type: "object", subtype: "node", objectId: "obj-1" } },
    "DOM.describeNode": { node: { backendNodeId: 7 } },
    "Accessibility.getFullAXTree": { nodes: PAGE },
  });
  const out = await a11y.tree({ tabId: 2, selector: "form" });
  assert.deepEqual(out.nodes.map((n) => n.role), ["form"]);
  assert.equal(sent[0][1].expression, 'document.querySelector("form")');
  assert.ok(sent.some(([m, p]) => m === "Runtime.releaseObject" && p.objectId === "obj-1"), "the handle is released");
});

test("a pruned root hands up its children, which is why nodes is a list", async () => {
  const { a11y } = load({
    "Runtime.evaluate": { result: { objectId: "o" } },
    "DOM.describeNode": { node: { backendNodeId: 4 } },
    "Accessibility.getFullAXTree": { nodes: PAGE },
  });
  const out = await a11y.tree({ tabId: 2, selector: "div" });
  assert.deepEqual(out.nodes.map((n) => n.role), ["heading", "form"]);
});

test("a selector that matches nothing, or is not one, is an error", async () => {
  const none = load({ "Runtime.evaluate": { result: { type: "object", subtype: "null", value: null } } });
  await assert.rejects(none.a11y.tree({ tabId: 2, selector: "#nope" }), /no element matches #nope/);
  const bad = load({ "Runtime.evaluate": { exceptionDetails: { text: "SyntaxError" } } });
  await assert.rejects(bad.a11y.tree({ tabId: 2, selector: "[" }), /invalid selector \[/);
  const outside = load({
    "Runtime.evaluate": { result: { objectId: "o" } },
    "DOM.describeNode": { node: { backendNodeId: 999 } },
    "Accessibility.getFullAXTree": { nodes: PAGE },
  });
  await assert.rejects(outside.a11y.tree({ tabId: 2, selector: "script" }), /not in the accessibility tree/);
  await assert.rejects(load().a11y.tree({ tabId: 2, depth: 0.5 }), /depth must be 1 to 100/);
});

// ── values a11y_tree never hands out (security review) ───────────────────

/** A login form as Chrome lists it: Chrome shows the password as bullets, its length kept. */
const LOGIN = [
  { nodeId: "1", ignored: false, role: v("RootWebArea"), name: v("Log in"), childIds: ["2", "3", "5", "6", "7", "8"], backendDOMNodeId: 1 },
  { nodeId: "2", ignored: false, role: v("textbox"), name: v("Email"), value: { type: "string", value: "ann@example.com" }, childIds: [], parentId: "1", backendDOMNodeId: 2 },
  { nodeId: "3", ignored: false, role: v("textbox"), name: v("Password"), value: { type: "string", value: "•••••••" }, childIds: ["4"], parentId: "1", backendDOMNodeId: 3 },
  { nodeId: "4", ignored: false, role: v("StaticText"), name: v("•••••••"), childIds: [], parentId: "3", backendDOMNodeId: 4 },
  { nodeId: "5", ignored: false, role: v("textbox"), name: v("Card"), value: { type: "string", value: "4111 1111 1111 1111" }, childIds: [], parentId: "1", backendDOMNodeId: 5 },
  { nodeId: "6", ignored: false, role: v("textbox"), name: v("Order"), value: { type: "string", value: "1234567812345678" }, childIds: [], parentId: "1", backendDOMNodeId: 6 },
  { nodeId: "7", ignored: false, role: v("textbox"), name: v("Code"), value: { type: "string", value: "829114" }, childIds: [], parentId: "1", backendDOMNodeId: 7 },
  { nodeId: "8", ignored: false, role: v("textbox"), name: v("PIN"), value: { type: "string", value: "4321" }, childIds: [], parentId: "1", backendDOMNodeId: 8 },
];
const ELEMENTS = {
  2: { localName: "input", attributes: ["type", "email", "name", "email"] },
  3: { localName: "input", attributes: ["type", "password", "name", "pw"] },
  5: { localName: "input", attributes: ["name", "ccnum"] },
  6: { localName: "input", attributes: ["name", "order"] },
  7: { localName: "input", attributes: ["autocomplete", "one-time-code"] },
  8: { localName: "input", attributes: ["id", "user_pin"] },
};

test("password, card-like and one-time-code values come back as •••, the field's text with them", async () => {
  const { a11y, sent } = load({
    "Accessibility.getFullAXTree": { nodes: LOGIN },
    "DOM.describeNode": (p) => ({ node: Object.assign({ backendNodeId: p.backendNodeId }, ELEMENTS[p.backendNodeId]) }),
  });
  const out = await a11y.tree({ tabId: 2 });
  const fields = Object.fromEntries(out.nodes[0].children.map((n) => [n.name, n]));
  assert.equal(fields.Email.value, "ann@example.com");
  assert.equal(fields.Order.value, "1234567812345678", "a number that fails Luhn is not a card");
  for (const name of ["Password", "Card", "Code", "PIN"]) assert.equal(fields[name].value, "•••", name);
  assert.equal("children" in fields.Password, false, "no text child repeats the bullets");
  assert.equal(JSON.stringify(out).match(/•••••••|4111|829114|4321/), null, "neither a value nor a password's length");
  assert.ok(!sent.some(([m, p]) => m === "DOM.describeNode" && p.backendNodeId === 3), "Chrome's bullets mask it without a lookup");
});

test("a field that cannot be looked up is masked, and so is a card number whatever the field", async () => {
  const { a11y } = load({
    "Accessibility.getFullAXTree": { nodes: LOGIN.slice(0, 2).concat([{ ...LOGIN[4], name: v("Notes"), backendDOMNodeId: 9 }]) },
    "DOM.describeNode": (p) => {
      if (p.backendNodeId === 2) throw new Error("No node with given id found");
      return { node: { localName: "textarea", attributes: ["name", "notes"] } };
    },
  });
  const out = await a11y.tree({ tabId: 2 });
  assert.deepEqual(out.nodes[0].children.map((n) => [n.name, n.value]), [["Email", "•••"], ["Notes", "•••"]]);
});
