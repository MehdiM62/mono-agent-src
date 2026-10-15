/**
 * MonoAgent Bridge — list_tabs and tab_group (plan C1)
 *
 * The coder needs to see which tabs exist and to keep the ones it opens
 * together: every tab an agent opens goes into a group titled
 * "Coder · <label>" (plan C2), so the person can see at a glance which tabs
 * an agent is driving, and the CLI can tell them apart from the person's own.
 *
 * Plain chrome.tabs / chrome.tabGroups; no debugger. Node-tested against a
 * fake chrome (agent_tabs.test.mjs).
 */

(function (root) {
  "use strict";

  // chrome.tabGroups.Color, the only values Chrome accepts.
  const COLORS = ["grey", "blue", "red", "yellow", "green", "pink", "purple", "cyan", "orange"];
  const MAX_TITLE = 80;

  /** listTabs answers list_tabs: every tab of every window, with its group's title. */
  async function listTabs() {
    const [tabs, groups] = await Promise.all([
      chrome.tabs.query({}),
      chrome.tabGroups ? chrome.tabGroups.query({}) : Promise.resolve([]),
    ]);
    const titles = new Map(groups.map((g) => [g.id, g.title || ""]));
    const groupOf = (t) => (Number.isInteger(t.groupId) ? t.groupId : -1);
    return {
      tabs: tabs.map((t) => ({
        tabId: t.id,
        windowId: t.windowId,
        groupId: groupOf(t),
        groupTitle: titles.get(groupOf(t)) || "",
        url: t.url || t.pendingUrl || "",
        title: t.title || "",
        active: !!t.active,
      })),
    };
  }

  /**
   * tabGroup answers tab_group: the tab joins the group with exactly this
   * title in its own window, which is made when there is none. A color is
   * applied either way; without one a new group gets Chrome's choice.
   */
  async function tabGroup(params) {
    const tabId = root.MonoAgentCdp.tabOf(params, "tab_group");
    const title = typeof params.title === "string" ? params.title.trim() : "";
    if (!title) throw new Error("tab_group: title is required");
    if (title.length > MAX_TITLE) throw new Error(`tab_group: title is longer than ${MAX_TITLE} characters`);
    const color = params.color || "";
    if (color && !COLORS.includes(color)) {
      throw new Error(`tab_group: color must be one of ${COLORS.join(", ")}`);
    }
    if (!chrome.tabGroups) throw new Error("tab_group: this browser has no tab groups");

    const tab = await chrome.tabs.get(tabId);
    // Matched here rather than by query({title}): Chrome reads that title as
    // a pattern, so a "*" in it would match other groups.
    const groups = await chrome.tabGroups.query({ windowId: tab.windowId });
    const existing = groups.find((g) => g.title === title);
    let groupId;
    if (existing) {
      groupId = existing.id;
      if (tab.groupId !== groupId) await chrome.tabs.group({ groupId, tabIds: [tabId] });
      if (color && existing.color !== color) await chrome.tabGroups.update(groupId, { color });
    } else {
      groupId = await chrome.tabs.group({ tabIds: [tabId], createProperties: { windowId: tab.windowId } });
      const update = { title };
      if (color) update.color = color;
      await chrome.tabGroups.update(groupId, update);
    }
    return { groupId };
  }

  root.MonoAgentTabs = { listTabs, tabGroup, COLORS };
})(globalThis);
