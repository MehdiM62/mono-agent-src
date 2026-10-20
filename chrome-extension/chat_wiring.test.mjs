// The worker's wiring for chat, read from background.js (issue #451). The
// bridge is tested directly elsewhere, so a missing importScripts or install
// would pass every other test. Crude on purpose.
// `CHROME_PATH=/nonexistent node --test chrome-extension/chat_wiring.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const HERE = dirname(fileURLToPath(import.meta.url));
const background = readFileSync(join(HERE, "background.js"), "utf8");
const count = (needle) => background.split(needle).length - 1;

test("the worker loads chat after ask.js and summary_ai.js", () => {
  const chat = background.indexOf('importScripts("chat_core.js", "chat_bridge.js")');
  assert.ok(chat !== -1, "chat_core.js and chat_bridge.js are not imported");
  assert.ok(chat > background.indexOf('importScripts("ask.js"'));
  assert.ok(chat > background.indexOf('"summary_ai.js"'));
});

test("the worker installs chat once, after the recall group installed ask.js", () => {
  assert.equal(count("MonoChat.install("), 1);
  assert.ok(background.indexOf("MonoChat.install(") > background.indexOf("MonoRecall.install("));
});

test("the worker loads and installs the form filler after chat", () => {
  assert.ok(background.indexOf('importScripts("form_bridge.js")') > background.indexOf('importScripts("chat_core.js", "chat_bridge.js")'));
  assert.equal(count("MonoForm.install("), 1);
});

test("the worker loads the page-action kinds before the proposals, and installs the page-action bridge once, after the agent commands", () => {
  const kinds = background.indexOf('"chat_proposals_act.js", "chat_proposals.js"');
  assert.ok(kinds !== -1 && kinds > background.indexOf("private_net.js"), "chat_proposals_act.js must load just before chat_proposals.js");
  const load = background.indexOf('importScripts("page_action_page.js", "page_action_cdp.js", "page_action_bridge.js")');
  assert.ok(load > background.indexOf('importScripts("agent_cdp.js"'), "the page actions use agent_cdp.js's attach and send");
  assert.equal(count("MonoPageActionBridge.install("), 1);
  assert.ok(background.indexOf("MonoPageActionBridge.install(") > background.indexOf("MonoAgentCommands.install("), "agent_cdp.js is installed first");
});

test("the side panel loads the page-action kinds before the proposals", () => {
  const html = readFileSync(join(HERE, "sidepanel.html"), "utf8");
  const act = html.indexOf('<script src="chat_proposals_act.js">');
  assert.ok(act !== -1 && act < html.indexOf('<script src="chat_proposals.js">'));
});
