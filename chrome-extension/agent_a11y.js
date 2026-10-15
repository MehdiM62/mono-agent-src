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
 * parent's name or value is dropped too. The main frame only; an iframe's content is
 * not in Chrome's tree for this frame.
 *
 * Node-tested on canned trees (agent_a11y.test.mjs).
 */

(function (root) {
  "use strict";

  const MAX_NODES = 5000;
  const MAX_DEPTH = 100;
  const MAX_TEXT = 1000;
  const FLATTEN = new Set(["generic", "none", "presentation"]);

  const text = (v) => {
    const s = v && v.value !== undefined && v.value !== null ? String(v.value) : "";
    return s.length > MAX_TEXT ? s.slice(0, MAX_TEXT) + "…" : s;
  };

  /**
   * prune turns Chrome's flat node list into the kept tree, starting at the
   * node for rootBackendId (the whole document when null).
   */
  function prune(axNodes, { rootBackendId = null, depth = 0, maxNodes = MAX_NODES } = {}) {
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
      const value = text(n.value);
      if (role === "StaticText" && parent.includes(name)) return;
      const node = { role, name };
      if (value) node.value = value;
      count++;
      out.push(node);
      if (depth && level >= depth) return;
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
    return prune(nodes || [], { rootBackendId, depth });
  }

  root.MonoAgentA11y = { tree, prune };
})(globalThis);
