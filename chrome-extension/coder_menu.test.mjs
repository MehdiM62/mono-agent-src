// The coder's right-click items (coder_menu.js): created once, under the
// capture group's "MonoAgent" parent even though that registrar empties and
// refills the menu as the worker starts, and routed to the handoff. Plus the
// worker's wiring of the coder modules, read from background.js.
// `CHROME_PATH=/nonexistent node --test chrome-extension/coder_menu.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { loadExtensionScripts } from "./test_helpers.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const TAB = { id: 42, windowId: 7, url: "https://shop.example/", title: "Shop" };
const tick = () => new Promise((r) => setTimeout(r, 5));

/**
 * fakeMenus is chrome.contextMenus as a worker sees it: each call is handled
 * by the browser in the order it was made, and its callback runs after,
 * also in order, with runtime.lastError set for a call that failed.
 */
function fakeMenus(chrome) {
  const items = new Map();
  const queue = [];
  const listeners = [];
  let pumping = false;
  const errors = [];
  function pump() {
    if (pumping) return;
    pumping = true;
    setTimeout(() => {
      while (queue.length) {
        const { apply, cb } = queue.shift();
        const err = apply();
        if (err) errors.push(err);
        chrome.runtime.lastError = err ? { message: err } : undefined;
        if (cb) cb();
        chrome.runtime.lastError = undefined;
      }
      pumping = false;
    }, 0);
  }
  const call = (apply, cb) => {
    queue.push({ apply, cb });
    pump();
  };
  return {
    items,
    errors,
    listeners,
    api: {
      removeAll: (cb) => call(() => void items.clear(), cb),
      update: (id, _props, cb) => call(() => (items.has(id) ? undefined : `Cannot find menu item with id ${id}`), cb),
      create: (item, cb) =>
        call(() => {
          if (items.has(item.id)) return `Cannot create item with duplicate id ${item.id}`;
          if (item.parentId && !items.has(item.parentId)) return `Cannot find menu item with id ${item.parentId}`;
          items.set(item.id, item);
          return undefined;
        }, cb),
      onClicked: { addListener: (fn) => listeners.push(fn) },
    },
  };
}

function setup() {
  const chrome = { runtime: { onMessage: { addListener() {} }, lastError: undefined }, commands: { onCommand: { addListener() {} } } };
  const menus = fakeMenus(chrome);
  chrome.contextMenus = menus.api;
  const sandbox = loadExtensionScripts(["capture_modes.js", "youtube_video.js", "capture_bridge.js", "coder_bridge.js", "coder_menu.js"], { chrome });
  const handoffs = [];
  sandbox.MonoCoderBridge.handoff = (tab, opts) => {
    handoffs.push({ tab, opts });
    return Promise.resolve({});
  };
  return { sandbox, menus, handoffs, M: sandbox.MonoCoderMenu };
}

test("the items land under the MonoAgent parent although the capture registrar empties the menu first", async () => {
  const { sandbox, menus, M } = setup();
  // The worker's order: the capture group registers, then the coder menu.
  sandbox.MonoCaptureBridge.install({});
  M.install();
  await tick();
  for (const item of M.menuItems()) {
    assert.ok(menus.items.has(item.id), `${item.id} was not created`);
    assert.equal(menus.items.get(item.id).parentId, "monoagent-root");
  }
  assert.ok(menus.items.has("monoagent-capture-full"), "the capture items are gone");
  assert.ok(!menus.errors.some((e) => /monoagent-coder/.test(e) || /duplicate/.test(e)), menus.errors.join("\n"));
});

test("the items and their click handler are registered once, however often install runs", async () => {
  const { sandbox, menus, M } = setup();
  sandbox.MonoCaptureBridge.install({});
  M.install();
  M.install();
  await tick();
  const coder = [...menus.items.keys()].filter((id) => id.startsWith("monoagent-coder"));
  assert.deepEqual(coder.sort(), Object.values(M.IDS).sort());
  // the capture bridge's listener and the coder menu's, no second coder one
  assert.equal(menus.listeners.length, 2);
});

test("each item hands off with its kind; the element item names the right-clicked element and its frame", async () => {
  const { M, handoffs } = setup();
  M.handleMenuClick({ menuItemId: M.IDS.element, frameId: 0 }, TAB);
  M.handleMenuClick({ menuItemId: M.IDS.bug, frameId: 0 }, TAB);
  M.handleMenuClick({ menuItemId: M.IDS.tokens, frameId: 2 }, TAB);
  assert.deepEqual(
    handoffs.map((h) => [h.tab.id, h.opts]),
    [
      [42, { which: "context", frameId: 0 }],
      [42, { kind: "bug-report" }],
      [42, { kind: "tokens" }],
    ]
  );
});

test("anyone else's item, and a click with no tab, are not the coder's", () => {
  const { M, handoffs } = setup();
  assert.equal(M.handleMenuClick({ menuItemId: "monoagent-capture-full" }, TAB), null);
  assert.equal(M.handleMenuClick({ menuItemId: "monoagent-tasks-page" }, TAB), null);
  assert.equal(M.handleMenuClick({ menuItemId: M.IDS.element }, undefined), null);
  assert.equal(M.handleMenuClick({ menuItemId: M.IDS.separator }, TAB), null);
  assert.equal(handoffs.length, 0);
});

test("the worker loads the coder after ask.js and the capture group, and installs each once", () => {
  const background = readFileSync(join(HERE, "background.js"), "utf8");
  const count = (needle) => background.split(needle).length - 1;
  const at = background.indexOf('importScripts("coder_bridge.js", "coder_menu.js")');
  assert.ok(at !== -1, "coder_bridge.js and coder_menu.js are not imported");
  assert.ok(at > background.indexOf('importScripts("ask.js"'));
  assert.ok(at > background.indexOf('"capture_modes.js"'));
  assert.equal(count("MonoCoderBridge.install("), 1);
  assert.equal(count("MonoCoderMenu.install("), 1);
  assert.ok(background.indexOf("MonoCoderMenu.install(") > background.indexOf("MonoCaptureBridge.install("), "the menu must come after the capture registrar");
  assert.match(background, /MonoCoderBridge\.install\(\{[^}]*pickElement[^}]*\}\)/, "the picker is not handed over");
});

test("the side panel loads the Coder section after the chat renderers it reuses", () => {
  const html = readFileSync(join(HERE, "sidepanel.html"), "utf8");
  const pos = (s) => html.indexOf(s);
  assert.ok(pos('<script src="sidepanel_coder.js">') > pos('<script src="coder_mirror.js">'));
  assert.ok(pos('<script src="coder_mirror.js">') > pos('<script src="chat_render.js">'));
  assert.ok(pos('<script src="coder_mirror.js">') > pos('<script src="sidepanel.js">'));
  assert.ok(pos('href="sidepanel_coder.css"') !== -1);
});
