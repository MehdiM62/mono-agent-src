/**
 * MonoAgent Bridge — a11y_tree (plan C1)
 *
 * The page as a screen reader gets it: roles, accessible names and values.
 * For the coder that is the cheapest faithful description of a page (far
 * smaller than its HTML) and the check that a control it built is labelled.
 *
 * Accessibility.getFullAXTree, then pruned to what carries meaning: ignored
 * nodes, inline text boxes and unnamed generic containers go, and their
 * children move up to the nearest kept ancestor. Text that only repeats its
 * parent's name or value is dropped too. The main frame only; an iframe's
 * content is not in Chrome's tree for this frame.
 *
 * A field's value is handed out only when the submit guard would hand it
 * out: a password, hidden or card-like field (MonoAgentRedact.sensitiveField,
 * read from the element through DOM.describeNode), a value that holds a card
 * number, or one Chrome itself shows as bullets, becomes "•••", and the
 * field's text children go with it. Chrome masks a password's value but
 * keeps its length; "•••" does not. A field that cannot be looked up is
 * masked too.
 *
 * Node-tested on canned trees (agent_a11y.test.mjs).
 */

(function (root) {
  "use strict";

  const MAX_NODES = 5000;
  const MAX_DEPTH = 100;
  const MAX_TEXT = 1000;
  // Fields looked up for masking; any beyond are masked unseen.
  const MAX_LOOKUPS = 300;
  const FLATTEN = new Set(["generic", "none", "presentation"]);

  const text = (v) => {
    const s = v && v.value !== undefined && v.value !== null ? String(v.value) : "";
    return s.length > MAX_TEXT ? s.slice(0, MAX_TEXT) + "…" : s;
  };

  /**
   * prune turns Chrome's flat node list into the kept tree, starting at the
   * node for rootBackendId (the whole document when null).
   */
  function prune(axNodes, { rootBackendId = null, depth = 0, maxNodes = MAX_NODES, masked = new Set() } = {}) {
    const byId = new Map(axNodes.map((n) => [n.nodeId, n]));
    let start;
    if (rootBackendId !== null) start = axNodes.find((n) => n.backendDOMNodeId === rootBackendId);
    else start = axNodes.find((n) => !n.parentId || !byId.has(n.parentId));
    if (!start) {
      throw new Error(rootBackendId !== null ? "a11y_tree: the element is not in the accessibility tree" : "a11y_tree: the page has no accessibility tree");
    }
    let count = 0;
    let truncated = false;

    // parent: the kept ancestor's name and value, which its text children repeat.
    function visit(n, level, parent, out) {
      if (count >= maxNodes) {
        truncated = true;
        return;
      }
      const role = text(n.role);
      const name = text(n.name);
      const kids = (n.childIds || []).map((id) => byId.get(id)).filter(Boolean);
      const lift = n.ignored || role === "InlineTextBox" || (FLATTEN.has(role) && !name);
      if (lift) {
        for (const k of kids) visit(k, level, parent, out);
        return;
      }
      const hidden = masked.has(n.nodeId);
      const value = hidden ? root.MonoAgentRedact.MASK : text(n.value);
      if (role === "StaticText" && parent.includes(name)) return;
      const node = { role, name };
      if (value) node.value = value;
      count++;
      out.push(node);
      if (hidden || (depth && level >= depth)) return;
      const children = [];
      for (const k of kids) visit(k, level + 1, [name, value], children);
      if (children.length) node.children = children;
    }

    const nodes = [];
    visit(start, 1, [], nodes);
    const result = { nodes };
    if (truncated) result.truncated = true;
    return result;
  }

  /** sensitiveNodes lists the AX nodes whose value must not be handed out. */
  async function sensitiveNodes(send, axNodes) {
    const R = root.MonoAgentRedact;
    const masked = new Set();
    let lookups = 0;
    for (const n of axNodes) {
      const value = n.value && n.value.value !== undefined && n.value.value !== null ? String(n.value.value) : "";
      if (!value) continue;
      if (/^[•●*]+$/.test(value) || R.cardNumberIn(value) || !Number.isInteger(n.backendDOMNodeId) || ++lookups > MAX_LOOKUPS) {
        masked.add(n.nodeId);
        continue;
      }
      try {
        const { node } = await send("DOM.describeNode", { backendNodeId: n.backendDOMNodeId });
        const a = {};
        const list = (node && node.attributes) || [];
        for (let i = 0; i + 1 < list.length; i += 2) a[list[i].toLowerCase()] = list[i + 1];
        const type = node && node.localName === "input" ? a.type || "text" : (node && node.localName) || "";
        if (R.sensitiveField({ type, autocomplete: a.autocomplete, name: a.name, id: a.id, value })) masked.add(n.nodeId);
      } catch {
        masked.add(n.nodeId); // not looked up: not handed out
      }
    }
    return masked;
  }

  /** tree answers a11y_tree. */
  async function tree(params) {
    const tabId = root.MonoAgentCdp.tabOf(params, "a11y_tree");
    const send = (method, p) => root.MonoAgentCdp.send(tabId, method, p);
    const depth = params.depth === undefined || params.depth === null ? 0 : params.depth;
    if (!Number.isInteger(depth) || depth < 0 || depth > MAX_DEPTH) throw new Error(`a11y_tree: depth must be 1 to ${MAX_DEPTH}`);

    let rootBackendId = null;
    if (params.selector) {
      if (typeof params.selector !== "string") throw new Error("a11y_tree: selector must be a string");
      const res = await send("Runtime.evaluate", { expression: `document.querySelector(${JSON.stringify(params.selector)})` });
      if (res && res.exceptionDetails) throw new Error(`a11y_tree: invalid selector ${params.selector}`);
      const obj = res && res.result;
      if (!obj || !obj.objectId) throw new Error(`a11y_tree: no element matches ${params.selector}`);
      try {
        const described = await send("DOM.describeNode", { objectId: obj.objectId });
        rootBackendId = described.node.backendNodeId;
      } finally {
        send("Runtime.releaseObject", { objectId: obj.objectId }).catch(() => {});
      }
    }
    const { nodes } = await send("Accessibility.getFullAXTree", {});
    const masked = await sensitiveNodes(send, nodes || []);
    return prune(nodes || [], { rootBackendId, depth, masked });
  }

  root.MonoAgentA11y = { tree, prune };
})(globalThis);
