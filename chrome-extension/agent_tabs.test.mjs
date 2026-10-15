// Tests for list_tabs and tab_group (agent_tabs.js) against a fake chrome.
// `node --test 'chrome-extension/**/*.test.mjs'`.

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

/** A browser with two windows: tabs and groups as chrome.tabs/tabGroups report them. */
function fakeChrome() {
  const calls = [];
  const tabs = [
    { id: 1, windowId: 10, groupId: -1, url: "https://example.com/", title: "Example", active: true },
    { id: 2, windowId: 10, groupId: 50, url: "http://localhost:5173/", title: "App", active: false },
    { id: 3, windowId: 20, groupId: 60, url: "", pendingUrl: "http://localhost:3000/", title: "", active: true },
  ];
  const groups = [
    { id: 50, windowId: 10, title: "Coder · app", color: "blue" },
    { id: 60, windowId: 20, title: "Coder · app", color: "grey" },
    { id: 70, windowId: 10, title: "Coder * other", color: "red" },
  ];
  let nextGroup = 80;
  const chrome = {
    tabs: {
      query: async () => tabs.map((t) => ({ ...t })),
      get: async (id) => {
        const t = tabs.find((x) => x.id === id);
        if (!t) throw new Error(`No tab with id: ${id}.`);
        return { ...t };
      },
      group: async (opts) => {
        calls.push(["group", opts]);
        const groupId = opts.groupId ?? nextGroup++;
        if (opts.groupId === undefined) groups.push({ id: groupId, windowId: opts.createProperties.windowId, title: "", color: "grey" });
        for (const id of opts.tabIds) tabs.find((t) => t.id === id).groupId = groupId;
        return groupId;
      },
    },
    tabGroups: {
      query: async (q) => groups.filter((g) => q.windowId === undefined || g.windowId === q.windowId).map((g) => ({ ...g })),
      update: async (id, props) => {
        calls.push(["update", id, props]);
        Object.assign(groups.find((g) => g.id === id), props);
      },
    },
  };
  return { chrome, calls, tabs };
}

function load(chrome) {
  return loadExtensionScripts(["agent_cdp.js", "agent_tabs.js"], { chrome }).MonoAgentTabs;
}

test("list_tabs reports every tab with its group's title, and -1 for none", async () => {
  const { chrome } = fakeChrome();
  const { tabs } = await load(chrome).listTabs();
  assert.deepEqual(tabs, [
    { tabId: 1, windowId: 10, groupId: -1, groupTitle: "", url: "https://example.com/", title: "Example", active: true },
    { tabId: 2, windowId: 10, groupId: 50, groupTitle: "Coder · app", url: "http://localhost:5173/", title: "App", active: false },
    { tabId: 3, windowId: 20, groupId: 60, groupTitle: "Coder · app", url: "http://localhost:3000/", title: "", active: true },
  ]);
});

test("tab_group reuses the group with exactly that title in the tab's own window", async () => {
  const { chrome, calls, tabs } = fakeChrome();
  const out = await load(chrome).tabGroup({ tabId: 1, title: "Coder · app" });
  assert.deepEqual(out, { groupId: 50 }, "window 10's group, not window 20's");
  assert.deepEqual(calls, [["group", { groupId: 50, tabIds: [1] }]], "nothing renamed or recoloured");
  assert.equal(tabs[0].groupId, 50);
});

test("tab_group makes the group when the window has none, titled and coloured", async () => {
  const { chrome, calls } = fakeChrome();
  const out = await load(chrome).tabGroup({ tabId: 1, title: "Coder · *", color: "cyan" });
  assert.deepEqual(out, { groupId: 80 }, "a '*' is a title, not a pattern matching 'Coder * other'");
  assert.deepEqual(calls, [
    ["group", { tabIds: [1], createProperties: { windowId: 10 } }],
    ["update", 80, { title: "Coder · *", color: "cyan" }],
  ]);
});

test("tab_group recolours an existing group only when asked to", async () => {
  const { chrome, calls } = fakeChrome();
  await load(chrome).tabGroup({ tabId: 2, title: "Coder · app", color: "orange" });
  assert.deepEqual(calls, [["update", 50, { color: "orange" }]], "already in the group: only the colour changes");
});

test("tab_group refuses a missing title, a long one and a colour Chrome has not got", async () => {
  const tabs = load(fakeChrome().chrome);
  await assert.rejects(tabs.tabGroup({ tabId: 1 }), /title is required/);
  await assert.rejects(tabs.tabGroup({ tabId: 1, title: "   " }), /title is required/);
  await assert.rejects(tabs.tabGroup({ tabId: 1, title: "x".repeat(81) }), /longer than 80/);
  await assert.rejects(tabs.tabGroup({ tabId: 1, title: "a", color: "magenta" }), /color must be one of grey, blue/);
  await assert.rejects(tabs.tabGroup({ title: "a" }), /tabId is required/);
});
