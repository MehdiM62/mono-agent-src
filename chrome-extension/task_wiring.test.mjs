// The worker's wiring, read from background.js (task board, spec 11). Every
// other test drives the task bridge directly, so a missing importScripts,
// install or connected() call would pass them all. Crude on purpose.
// `CHROME_PATH=/nonexistent node --test chrome-extension/task_wiring.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const HERE = dirname(fileURLToPath(import.meta.url));
const background = readFileSync(join(HERE, "background.js"), "utf8");
const count = (needle) => background.split(needle).length - 1;

test("the worker loads the task outbox and bridge after ask.js", () => {
  const ask = background.indexOf('importScripts("ask.js"');
  const tasks = background.indexOf('importScripts("task_outbox.js", "task_bridge.js"');
  assert.ok(ask !== -1 && tasks > ask, "task_outbox.js and task_bridge.js are not imported after ask.js");
});

test("the worker installs the task bridge once, after the recall group installed ask.js", () => {
  assert.equal(count("MonoTaskBridge.install("), 1);
  assert.ok(background.indexOf("MonoTaskBridge.install(") > background.indexOf("MonoRecall.install("));
});

test("the worker sends what waited once the bridge on a new socket reports a sign-in, and on signing in", () => {
  const start = background.indexOf("ws.onopen = () => {");
  const end = background.indexOf("ws.onmessage = ", start);
  assert.ok(start !== -1 && end > start, "ws.onopen not found");
  assert.match(background.slice(end, background.indexOf("ws.onerror = ", end)), /if \(gate\.signedIn\(\)\) whileSignedIn\(\);/);
  const fn = background.indexOf("function whileSignedIn() {");
  assert.ok(fn !== -1, "whileSignedIn not found");
  assert.match(background.slice(fn, background.indexOf("\n}\n", fn)), /MonoTaskBridge\.connected\(\);/);
  assert.match(background, /gate\.onChange\(\(signedIn\) => \{[\s\S]*?whileSignedIn\(\);/);
});

test("the worker loads the task menu with the bridge and installs it once", () => {
  assert.match(background, /importScripts\("task_outbox\.js", "task_bridge\.js", "task_menu\.js"\);/);
  assert.equal(count("MonoTaskMenu.install("), 1);
  assert.ok(background.indexOf("MonoTaskMenu.install(") > background.indexOf("MonoTaskBridge.install("));
});
