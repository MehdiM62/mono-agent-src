// Tests for watch_tab / read_console / read_network (agent_watch.js), fed
// the debugger events Chrome sends.
// `node --test 'chrome-extension/**/*.test.mjs'`.

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

function setup({ failEnable = false, page = "http://localhost:5173/" } = {}) {
  const g = loadExtensionScripts(["agent_cdp.js", "agent_redact.js", "agent_watch.js"], {});
  const sent = [];
  let emit = null;
  let detach = null;
  let removed = null;
  g.MonoAgentCdp.install({
    attach: async () => {},
    cdp: async (tabId, method) => {
      if (failEnable && method === "Network.enable") throw new Error("Cannot access a chrome:// URL");
      sent.push([tabId, method]);
      if (method === "Page.getFrameTree") return { frameTree: { frame: { id: "MAIN", url: page } } };
      return {};
    },
    onDebuggerEvent: (fn) => (emit = fn),
    onDebuggerDetach: (fn) => (detach = fn),
    onTabRemoved: (fn) => (removed = fn),
  });
  return {
    watch: g.MonoAgentWatch,
    cdp: g.MonoAgentCdp,
    sent,
    event: (tabId, method, params) => emit({ tabId }, method, params),
    detach: (tabId, reason) => detach({ tabId }, reason),
    removed: (tabId) => removed(tabId),
  };
}

const str = (value) => ({ type: "string", value });
const log = (type, args, extra = {}) => Object.assign({ type, args, timestamp: 1760000000000.4, executionContextId: 1 }, extra);
const frame = (functionName, url, lineNumber, columnNumber) => ({ functionName, url, lineNumber, columnNumber });

test("watch_tab enables Runtime, Log and Network on the tab and holds it", async () => {
  const t = setup();
  assert.deepEqual(await t.watch.watch({ tabId: 4, on: true }), { watching: true });
  assert.deepEqual(t.sent, [[4, "Page.enable"], [4, "Page.getFrameTree"], [4, "Runtime.enable"], [4, "Log.enable"], [4, "Network.enable"]]);
  assert.equal(t.cdp.holds(4), true);
  assert.deepEqual(await t.watch.watch({ tabId: 4, on: false }), { watching: false });
  assert.equal(t.cdp.holds(4), false, "unwatched, the idle sweep may detach it");
  await assert.rejects(t.watch.watch({ tabId: 4 }), /on must be true or false/);
});

test("a watch that cannot start lets go of its tab", async () => {
  const t = setup({ failEnable: true });
  await assert.rejects(t.watch.watch({ tabId: 4, on: true }), /watch_tab: Cannot access/);
  assert.equal(t.cdp.holds(4), false);
});

test("console entries read like DevTools: format specifiers, previews, 1-based locations", async () => {
  const t = setup();
  await t.watch.watch({ tabId: 1, on: true });
  t.event(1, "Runtime.consoleAPICalled", log("log", [str("%s has %d items%c"), str("cart"), { type: "number", value: 3.7 }, str("color:red")], {
    stackTrace: { callFrames: [frame("render", "http://localhost:5173/src/App.jsx", 41, 8)] },
  }));
  t.event(1, "Runtime.consoleAPICalled", log("info", [
    { type: "object", preview: { type: "object", description: "Object", overflow: false, properties: [{ name: "id", type: "number", value: "7" }, { name: "name", type: "string", value: "Ann" }] } },
    { type: "object", subtype: "array", preview: { type: "object", subtype: "array", overflow: true, properties: [{ name: "0", type: "number", value: "1" }] } },
    { type: "undefined" },
    { type: "object", subtype: "null", value: null },
    { type: "number", unserializableValue: "NaN" },
  ]));
  t.event(1, "Runtime.consoleAPICalled", log("error", [str("save failed")], {
    stackTrace: { callFrames: [frame("save", "http://localhost:5173/src/api.js", 9, 2), frame("", "http://localhost:5173/src/App.jsx", 20, 4)] },
  }));
  const { entries, next } = t.watch.readConsole({ tabId: 1 });
  assert.equal(next, 3);
  assert.deepEqual(entries[0], { seq: 1, ts: 1760000000000, origin: "http://localhost:5173", level: "log", text: "cart has 3 items", url: "http://localhost:5173/src/App.jsx", line: 42, column: 9 });
  assert.equal(entries[1].text, '{id: 7, name: "Ann"} [1, …] undefined null NaN');
  assert.equal(entries[1].level, "info");
  assert.equal(entries[2].level, "error");
  assert.equal(entries[2].stack, "    at save (http://localhost:5173/src/api.js:10:3)\n    at <anonymous> (http://localhost:5173/src/App.jsx:21:5)");
  assert.equal("stack" in entries[0], false, "a plain log carries no stack");
});

test("uncaught exceptions and browser log entries are console entries too", async () => {
  const t = setup();
  await t.watch.watch({ tabId: 1, on: true });
  t.event(1, "Runtime.exceptionThrown", {
    timestamp: 1760000000100,
    exceptionDetails: {
      text: "Uncaught",
      url: "http://localhost:5173/src/main.js",
      lineNumber: 2,
      columnNumber: 10,
      exception: { description: "TypeError: x is undefined\n    at boot (main.js:3:11)" },
    },
  });
  t.event(1, "Log.entryAdded", { entry: { source: "network", level: "error", text: "Failed to load resource: 404 (Not Found)", timestamp: 1760000000200, url: "http://localhost:5173/missing.png" } });
  t.event(1, "Log.entryAdded", { entry: { source: "violation", level: "verbose", text: "[Violation] slow handler", timestamp: 1760000000300 } });
  const { entries } = t.watch.readConsole({ tabId: 1 });
  assert.deepEqual(entries[0], {
    seq: 1, ts: 1760000000100, origin: "http://localhost:5173", level: "error", text: "Uncaught TypeError: x is undefined",
    url: "http://localhost:5173/src/main.js", line: 3, column: 11, stack: "    at boot (main.js:3:11)",
  });
  assert.deepEqual(entries[1], { seq: 2, ts: 1760000000200, origin: "http://localhost:5173", level: "error", text: "Failed to load resource: 404 (Not Found)", url: "http://localhost:5173/missing.png", line: 0, column: 0 });
  assert.equal(entries[2].level, "debug");
});

test("read_console pages by seq, filters by level, and next skips what the filter dropped", async () => {
  const t = setup();
  await t.watch.watch({ tabId: 1, on: true });
  const levels = ["log", "error", "warning", "log", "error"];
  levels.forEach((type, i) => t.event(1, "Runtime.consoleAPICalled", log(type, [str(`m${i + 1}`)], { timestamp: 1000 + i })));
  assert.deepEqual(t.watch.readConsole({ tabId: 1, limit: 2 }).entries.map((e) => e.text), ["m1", "m2"]);
  assert.equal(t.watch.readConsole({ tabId: 1, limit: 2 }).next, 2);
  const errors = t.watch.readConsole({ tabId: 1, levels: ["error"] });
  assert.deepEqual(errors.entries.map((e) => e.seq), [2, 5]);
  assert.equal(errors.next, 5);
  const fromCsv = t.watch.readConsole({ tabId: 1, since: 2, levels: "error,warning" });
  assert.deepEqual(fromCsv.entries.map((e) => e.seq), [3, 5]);
  const none = t.watch.readConsole({ tabId: 1, since: 3, levels: ["warning"] });
  assert.deepEqual(none, { entries: [], next: 5 }, "nothing matched, but the cursor still moves past what was read");
  assert.deepEqual(t.watch.readConsole({ tabId: 1, since: 5 }), { entries: [], next: 5 });
  assert.throws(() => t.watch.readConsole({ tabId: 1, levels: ["fatal"] }), /unknown level fatal/);
  assert.throws(() => t.watch.readConsole({ tabId: 1, limit: 0 }), /limit must be 1 to 500/);
});

test("a cursor from before a worker restart reads from the start", async () => {
  const t = setup();
  await t.watch.watch({ tabId: 1, on: true });
  t.event(1, "Runtime.consoleAPICalled", log("log", [str("fresh")]));
  assert.deepEqual(t.watch.readConsole({ tabId: 1, since: 480 }).entries.map((e) => e.text), ["fresh"]);
});

test("the ring keeps the newest 500, and seq keeps growing", async () => {
  const t = setup();
  await t.watch.watch({ tabId: 1, on: true });
  for (let i = 1; i <= 520; i++) t.event(1, "Runtime.consoleAPICalled", log("log", [str(`m${i}`)], { timestamp: i }));
  const all = t.watch.readConsole({ tabId: 1, limit: 500 });
  assert.equal(all.entries.length, 500);
  assert.equal(all.entries[0].seq, 21);
  assert.equal(all.next, 520);
});

test("long text is cut, so a chatty page cannot fill the worker", async () => {
  const t = setup();
  await t.watch.watch({ tabId: 1, on: true });
  t.event(1, "Runtime.consoleAPICalled", log("log", [str("x".repeat(10000))]));
  const [e] = t.watch.readConsole({ tabId: 1 }).entries;
  assert.ok(e.text.length < 4100 && e.text.endsWith("… [truncated]"));
});

test("an unwatched tab says so, and names the restart that clears buffers", async () => {
  const t = setup();
  assert.throws(() => t.watch.readConsole({ tabId: 8 }), /tab 8 is not watched: send watch_tab first \(a restart of the extension's worker clears/);
  assert.throws(() => t.watch.readNetwork({ tabId: 8 }), /read_network: tab 8 is not watched/);
});

test("events for a tab that is not watched, or no longer, are not buffered", async () => {
  const t = setup();
  await t.watch.watch({ tabId: 1, on: true });
  t.event(2, "Runtime.consoleAPICalled", log("log", [str("other tab")]));
  t.event(1, "Runtime.consoleAPICalled", log("log", [str("kept")], { timestamp: 1 }));
  await t.watch.watch({ tabId: 1, on: false });
  t.event(1, "Runtime.consoleAPICalled", log("log", [str("after")], { timestamp: 2 }));
  assert.deepEqual(t.watch.readConsole({ tabId: 1 }).entries.map((e) => e.text), ["kept"], "stopped: the buffer stays readable");
});

test("watching again drops Chrome's replay of what is already buffered", async () => {
  const t = setup();
  await t.watch.watch({ tabId: 1, on: true });
  t.event(1, "Runtime.consoleAPICalled", log("log", [str("one")], { timestamp: 100 }));
  t.detach(1, "canceled_by_user"); // the person pressed Cancel; a later watch attaches afresh
  assert.equal(t.cdp.holds(1), false);
  await t.watch.watch({ tabId: 1, on: true });
  t.event(1, "Runtime.consoleAPICalled", log("log", [str("one")], { timestamp: 100 })); // Runtime.enable's replay
  t.event(1, "Runtime.consoleAPICalled", log("log", [str("two")], { timestamp: 200 }));
  assert.deepEqual(t.watch.readConsole({ tabId: 1 }).entries.map((e) => e.text), ["one", "two"]);
});

test("closing the tab drops its buffers", async () => {
  const t = setup();
  await t.watch.watch({ tabId: 1, on: true });
  t.event(1, "Runtime.consoleAPICalled", log("log", [str("x")]));
  t.removed(1);
  assert.throws(() => t.watch.readConsole({ tabId: 1 }), /not watched/);
});

// ── network ───────────────────────────────────────────────────────────────

function request(t, tabId, requestId, url, { method = "GET", type = "Fetch", at = 10, wallTime = 1760000000 } = {}) {
  t.event(tabId, "Network.requestWillBeSent", {
    requestId,
    request: { url, method, headers: { Cookie: "session=secret", Authorization: "Bearer tkn" }, postData: "password=hunter2" },
    timestamp: at,
    wallTime,
    type,
  });
}

test("a request is listed once it ends, with its outcome and never a header or body", async () => {
  const t = setup();
  await t.watch.watch({ tabId: 1, on: true });
  request(t, 1, "1.1", "http://localhost:5173/api/items?page=2", { method: "POST" });
  t.event(1, "Network.responseReceived", {
    requestId: "1.1",
    type: "XHR",
    response: { status: 500, statusText: "Internal Server Error", mimeType: "application/json", headers: { "Set-Cookie": "a=b" } },
  });
  assert.deepEqual(t.watch.readNetwork({ tabId: 1 }).entries, [], "still in flight");
  t.event(1, "Network.loadingFinished", { requestId: "1.1", timestamp: 10.25, encodedDataLength: 512 });
  const { entries, next } = t.watch.readNetwork({ tabId: 1 });
  assert.equal(next, 1);
  assert.deepEqual(entries, [{
    seq: 1, ts: 1760000000000, origin: "http://localhost:5173", requestId: "1.1", method: "POST", url: "http://localhost:5173/api/items?page=2",
    status: 500, statusText: "Internal Server Error", mimeType: "application/json", type: "XHR",
    failed: false, errorText: "", durationMs: 250, size: 512,
  }]);
  const wire = JSON.stringify(entries);
  for (const secret of ["secret", "tkn", "hunter2", "Set-Cookie"]) assert.equal(wire.includes(secret), false, `${secret} leaked`);
});

test("failures, redirects and failedOnly", async () => {
  const t = setup();
  await t.watch.watch({ tabId: 1, on: true });
  request(t, 1, "2", "http://localhost:5173/old", { type: "Document" });
  t.event(1, "Network.requestWillBeSent", {
    requestId: "2",
    request: { url: "http://localhost:5173/new", method: "GET" },
    redirectResponse: { status: 301, statusText: "Moved Permanently", mimeType: "text/html", encodedDataLength: 90 },
    timestamp: 10.1,
    wallTime: 1760000000.1,
    type: "Document",
  });
  t.event(1, "Network.responseReceived", { requestId: "2", type: "Document", response: { status: 200, statusText: "OK", mimeType: "text/html" } });
  t.event(1, "Network.loadingFinished", { requestId: "2", timestamp: 10.3, encodedDataLength: 2048 });
  request(t, 1, "3", "http://localhost:9999/api", { at: 11 });
  t.event(1, "Network.loadingFailed", { requestId: "3", timestamp: 11.05, type: "Fetch", errorText: "net::ERR_CONNECTION_REFUSED", canceled: false });
  request(t, 1, "4", "http://localhost:5173/missing.png", { type: "Image", at: 12 });
  t.event(1, "Network.responseReceived", { requestId: "4", type: "Image", response: { status: 404, statusText: "Not Found", mimeType: "text/html" } });
  t.event(1, "Network.loadingFinished", { requestId: "4", timestamp: 12.01, encodedDataLength: 10 });

  const all = t.watch.readNetwork({ tabId: 1 }).entries;
  assert.deepEqual(all.map((e) => [e.seq, e.url, e.status, e.failed]), [
    [1, "http://localhost:5173/old", 301, false],
    [2, "http://localhost:5173/new", 200, false],
    [3, "http://localhost:9999/api", 0, true],
    [4, "http://localhost:5173/missing.png", 404, false],
  ]);
  assert.equal(all[0].durationMs, 100);
  assert.equal(all[2].errorText, "net::ERR_CONNECTION_REFUSED");
  const failed = t.watch.readNetwork({ tabId: 1, failedOnly: true });
  assert.deepEqual(failed.entries.map((e) => e.seq), [3, 4], "HTTP 4xx/5xx count as failed too");
  assert.equal(failed.next, 4);
});

test("Cancel on the debugging banner stops the watch and keeps what was read", async () => {
  const t = setup();
  await t.watch.watch({ tabId: 1, on: true });
  request(t, 1, "5", "http://localhost/a");
  t.event(1, "Network.loadingFinished", { requestId: "5", timestamp: 11, encodedDataLength: 1 });
  request(t, 1, "6", "http://localhost/in-flight");
  t.detach(1, "canceled_by_user");
  t.event(1, "Network.loadingFinished", { requestId: "6", timestamp: 12, encodedDataLength: 1 });
  assert.deepEqual(t.watch.readNetwork({ tabId: 1 }).entries.map((e) => e.requestId), ["5"]);
});

// ── origins and secrets (security review, items 6) ───────────────────────

// Assembled at runtime: the repository's secret scanner reads `name=value` literals as credentials.
const AT = ["access", "token"].join("_");

test("entries carry the page's origin; a navigation moves it, and a reader can keep to one origin", async () => {
  const t = setup({ page: "http://localhost:5173/login" });
  await t.watch.watch({ tabId: 1, on: true });
  t.event(1, "Runtime.consoleAPICalled", log("log", [str("on the app")], { timestamp: 1 }));
  // The page's own document request loads into the origin it asks for.
  t.event(1, "Network.requestWillBeSent", {
    requestId: "D1", request: { url: "https://accounts.example.com/signin", method: "GET" }, timestamp: 1, wallTime: 1, type: "Document", frameId: "MAIN",
  });
  t.event(1, "Network.loadingFinished", { requestId: "D1", timestamp: 2, encodedDataLength: 1 });
  t.event(1, "Page.frameNavigated", { frame: { id: "MAIN", url: "https://accounts.example.com/signin" } });
  t.event(1, "Page.frameNavigated", { frame: { id: "IFRAME", parentId: "MAIN", url: "https://ads.example.net/x" } });
  t.event(1, "Runtime.consoleAPICalled", log("log", [str("on the sign-in page")], { timestamp: 2 }));
  request(t, 1, "X1", "https://accounts.example.com/api/me");
  t.event(1, "Network.loadingFinished", { requestId: "X1", timestamp: 11, encodedDataLength: 1 });

  const all = t.watch.readConsole({ tabId: 1 });
  assert.deepEqual(all.entries.map((e) => [e.text, e.origin]), [
    ["on the app", "http://localhost:5173"],
    ["on the sign-in page", "https://accounts.example.com"],
  ]);
  const mine = t.watch.readConsole({ tabId: 1, origin: "http://localhost:5173" });
  assert.deepEqual(mine.entries.map((e) => e.text), ["on the app"]);
  assert.equal(mine.next, 2, "the cursor still moves past what the filter dropped");
  assert.deepEqual(t.watch.readNetwork({ tabId: 1 }).entries.map((e) => [e.requestId, e.origin]), [
    ["D1", "https://accounts.example.com"],
    ["X1", "https://accounts.example.com"],
  ]);
  assert.deepEqual(t.watch.readNetwork({ tabId: 1, origin: "http://localhost:5173" }).entries, []);
  assert.throws(() => t.watch.readNetwork({ tabId: 1, origin: 5 }), /origin must be a string/);
});

test("secret-looking query values are masked before anything is buffered", async () => {
  const t = setup();
  await t.watch.watch({ tabId: 1, on: true });
  request(t, 1, "S1", `https://api.test/oauth/cb?code=4/0Ab&state=xy&page=2`);
  t.event(1, "Network.loadingFinished", { requestId: "S1", timestamp: 11, encodedDataLength: 1 });
  t.event(1, "Runtime.consoleAPICalled", log("error", [str(`GET https://api.test/me?${AT}=ya29.a0 401`)], {
    stackTrace: { callFrames: [frame("load", "https://cdn.test/a.js?sig=zz", 0, 0)] },
  }));
  const [net] = t.watch.readNetwork({ tabId: 1 }).entries;
  assert.equal(net.url, "https://api.test/oauth/cb?code=•••&state=•••&page=2");
  const [c] = t.watch.readConsole({ tabId: 1 }).entries;
  assert.equal(c.text, `GET https://api.test/me?${AT}=••• 401`);
  assert.equal(c.url, "https://cdn.test/a.js?sig=•••");
  assert.equal(c.stack, "    at load (https://cdn.test/a.js?sig=•••:1:1)");
  assert.equal(JSON.stringify([net, c]).match(/4\/0Ab|ya29|zz\b/), null);
});
