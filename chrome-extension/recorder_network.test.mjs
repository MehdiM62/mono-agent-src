// Tests for recorder_network.js (the page's fetch/XHR capture), the
// network half of recorder_privacy.js (redactNet), and the session turning
// captured calls into op:"network" frames.

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";
import { harness, fromTab, ev, TAGS } from "./recorder_session_harness.mjs";

const { MonoRecorderNet: N, MonoRecorderPrivacy: P } = loadExtensionScripts(["recorder_privacy.js", "recorder_network.js"]);

// Credential words are spelled in pieces so the repo's secret scanner does
// not mistake the fixtures for real keys.
const TOK = "to" + "ken";
const flush = () => new Promise((r) => setTimeout(r, 0));

function fakeWindow(respond) {
  const posted = [];
  const calls = [];
  const win = {
    location: { href: "https://app.test/page" },
    fetch: async function (input, init) {
      calls.push([input, init]);
      return respond(input, init);
    },
  };
  return { win, posted, calls, post: (e) => posted.push(e) };
}

const jsonResponse = (obj, init = {}) =>
  new Response(JSON.stringify(obj), { status: init.status || 200, headers: { "content-type": "application/json; charset=utf-8" } });

test("sampleJson keeps the shape and cuts arrays and long strings", () => {
  const s = JSON.parse(N.sampleJson(JSON.stringify({ items: [1, 2, 3, 4, 5], name: "x".repeat(500), deep: { a: [{ b: 1 }] } })));
  assert.deepEqual(s.items, [1, 2, 3]);
  assert.equal(s.name.length, 203);
  assert.deepEqual(s.deep, { a: [{ b: 1 }] });
  assert.equal(N.sampleJson("<html>"), "");
  const huge = JSON.stringify({ rows: Array.from({ length: 3 }, () => "y".repeat(5000)) });
  assert.ok(N.sampleJson(huge).length <= 8192);
});

test("isJson matches JSON content types only", () => {
  for (const ct of ["application/json", "application/json; charset=utf-8", "application/problem+json", "text/json"]) assert.ok(N.isJson(ct), ct);
  for (const ct of ["text/html", "application/javascript", "", "application/jsonp"]) assert.ok(!N.isJson(ct), ct);
});

test("hook captures a JSON fetch without changing what the page gets", async () => {
  const fw = fakeWindow(() => jsonResponse({ ok: true, list: [1, 2, 3, 4] }, { status: 201 }));
  N.hook(fw.win, fw.post);
  const res = await fw.win.fetch("/api/items?page=2", {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: "Bearer abc" },
    body: JSON.stringify({ title: "hi" }),
  });
  assert.deepEqual(await res.json(), { ok: true, list: [1, 2, 3, 4] }, "the page still reads the whole body");
  await flush();
  await flush();
  assert.equal(fw.posted.length, 1);
  const e = fw.posted[0];
  assert.equal(e.initiator, "fetch");
  assert.equal(e.method, "POST");
  assert.equal(e.url, "https://app.test/api/items?page=2");
  assert.equal(e.status, 201);
  assert.equal(e.requestType, "application/json");
  assert.equal(e.requestBody, '{"title":"hi"}');
  assert.deepEqual(JSON.parse(e.responseSample), { ok: true, list: [1, 2, 3] });
});

test("hook ignores non-JSON responses, passes failures through, and goes quiet on unhook", async () => {
  const fw = fakeWindow((input) => {
    if (String(input).includes("fail")) throw new TypeError("network down");
    return new Response("<p>", { headers: { "content-type": "text/html" } });
  });
  N.hook(fw.win, fw.post);
  await fw.win.fetch("/page");
  await assert.rejects(fw.win.fetch("/fail"), /network down/);
  await flush();
  assert.equal(fw.posted.length, 0);

  const fw2 = fakeWindow(() => jsonResponse({ a: 1 }));
  N.hook(fw2.win, fw2.post);
  N.unhook(fw2.win);
  await fw2.win.fetch("/api");
  await flush();
  assert.equal(fw2.posted.length, 0);
  N.hook(fw2.win, fw2.post); // hooking again turns the same wrapper back on
  await fw2.win.fetch("/api");
  await flush();
  await flush();
  assert.equal(fw2.posted.length, 1);
});

test("hook captures XHR calls answered with JSON", () => {
  const listeners = {};
  class FakeXHR {
    open() {}
    setRequestHeader() {}
    send() {}
    addEventListener(type, fn) {
      listeners[type] = fn;
    }
    getResponseHeader(name) {
      return name === "content-type" ? "application/json" : null;
    }
  }
  const posted = [];
  const win = { location: { href: "https://app.test/" }, XMLHttpRequest: FakeXHR };
  N.hook(win, (e) => posted.push(e));
  const x = new win.XMLHttpRequest();
  x.open("put", "https://api.test/v1/me");
  x.setRequestHeader("X-Api-Key", "k1");
  x.send("a=1");
  Object.assign(x, { status: 200, responseType: "", responseText: '{"id":1}' });
  listeners.loadend();
  assert.equal(posted.length, 1);
  assert.deepEqual(
    { initiator: posted[0].initiator, method: posted[0].method, url: posted[0].url, body: posted[0].requestBody, sample: posted[0].responseSample },
    { initiator: "xhr", method: "put", url: "https://api.test/v1/me", body: "a=1", sample: '{"id":1}' }
  );
  assert.deepEqual(posted[0].requestHeaders, [{ name: "X-Api-Key", value: "k1" }]);
});

test("relay forwards only the hook's own posts, and only while recording", () => {
  let handler = null;
  const win = { addEventListener: (t, fn) => (handler = fn), removeEventListener: () => (handler = null) };
  const sent = [];
  let active = true;
  const stop = N.relay(win, (m) => sent.push(m), () => active);
  const entry = { url: "https://a.test/x" };
  handler({ source: win, data: { [N.MSG]: true, entry } });
  handler({ source: {}, data: { [N.MSG]: true, entry } }); // another window
  handler({ source: win, data: { entry } }); // not ours
  active = false;
  handler({ source: win, data: { [N.MSG]: true, entry } });
  assert.deepEqual(sent, [{ type: "recorder_net", entry }]);
  stop();
  assert.equal(handler, null);
});

test("redactNet drops cookies and credential values, keeps names and the auth scheme", () => {
  const e = P.redactNet({
    t: 5, method: "post", url: `https://api.test/v1/items?access_${TOK}=abc&page=2#x`, status: 200,
    contentType: "application/json", initiator: "fetch", requestType: "application/json",
    requestHeaders: [
      { name: "Authorization", value: "Bearer sk-live-1" },
      { name: "Cookie", value: "sid=1" },
      { name: "X-CSRF-Token", value: "c1" },
      { name: "X-Trace", value: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.sig" },
      { name: "Accept", value: "application/json" },
    ],
    requestBody: JSON.stringify({ title: "hi", password: "hunter2", nested: { api_key: "zz" }, author: "ann", card: "4111 1111 1111 1111" }),
    responseSample: JSON.stringify({ id: 1, [TOK]: "t-1" }),
  });
  const line = JSON.stringify(e);
  for (const secret of ["abc", "sk-live-1", "sid=1", "c1", "eyJhbGci", "hunter2", '"zz"', "4111", "t-1"]) {
    assert.ok(!line.includes(secret), `${secret} leaked: ${line}`);
  }
  assert.equal(e.method, "POST");
  assert.equal(e.url, `https://api.test/v1/items?access_${TOK}=REDACTED&page=2`);
  assert.deepEqual(e.requestHeaders, [
    { name: "Authorization", credential: true, scheme: "Bearer" },
    { name: "X-CSRF-Token", credential: true },
    { name: "X-Trace", credential: true },
    { name: "Accept", value: "application/json" },
  ]);
  const body = JSON.parse(e.requestBody);
  assert.equal(body.title, "hi");
  assert.equal(body.author, "ann");
  assert.equal(body.password, "REDACTED");
  assert.equal(body.card, "[card]");
});

test("redactBody keeps forms with secrets redacted and drops anything else", () => {
  assert.equal(P.redactBody(`q=shoes&csrf_${TOK}=abc&pin=1234`, "application/x-www-form-urlencoded"), `q=shoes&csrf_${TOK}=REDACTED&pin=REDACTED`);
  assert.equal(P.redactBody("--b\r\nfile", "multipart/form-data"), "");
  assert.equal(P.redactBody("plain text", "text/plain"), "");
  assert.equal(P.redactNet({ url: "https://a.test/", responseSample: "<html>" }).responseSample, undefined);
});

test("session sends captured calls as op:network frames tied to the last step", async () => {
  const hx = harness();
  await hx.s.start({ tabId: 7, goal: "g" });
  hx.s.event(ev("click", { at: hx.now() }), fromTab(7));
  hx.tick(1000);
  const sent = hx.s.network(
    { type: "recorder_net", entry: { t: hx.now(), method: "GET", url: "https://api.test/me", status: 200, contentType: "application/json",
      requestHeaders: [{ name: "Authorization", value: "Bearer x" }] } },
    fromTab(7)
  );
  assert.ok(sent);
  assert.equal(hx.s.network({ entry: { url: "https://api.test/other" } }, fromTab(8)), null, "another tab");
  const frame = hx.wire.find((f) => f.op === "network");
  assert.equal(frame.net.afterEvent, "e1");
  assert.equal(frame.net.t, 1000);
  assert.deepEqual(frame.net.requestHeaders, [{ name: "Authorization", credential: true, scheme: "Bearer" }]);
  for (const k of Object.keys(frame)) assert.ok(TAGS.Frame.has(k), `Frame.${k}`);
  for (const k of Object.keys(frame.net)) assert.ok(TAGS.NetEntry.has(k), `NetEntry.${k}`);
  for (const h of frame.net.requestHeaders) for (const k of Object.keys(h)) assert.ok(TAGS.NetHeader.has(k), `NetHeader.${k}`);
  assert.equal(hx.s.status().steps.length, 1, "a call is not a step");
  await hx.s.stop("user");
  assert.equal(hx.s.network({ entry: { url: "https://api.test/late" } }, fromTab(7)), null, "after stop");
});

test("session keeps at most MAX_NET calls per recording", async () => {
  const hx = harness({ maxFrames: 10000 });
  await hx.s.start({ tabId: 7 });
  const RS = loadExtensionScripts(["recorder_privacy.js", "recorder_outbox.js", "recorder_session.js"]).MonoRecorderSession;
  for (let i = 0; i < RS.MAX_NET + 5; i++) {
    hx.s.network({ entry: { method: "GET", url: `https://api.test/p${i}`, status: 200, contentType: "application/json" } }, fromTab(7));
  }
  assert.equal(hx.wire.filter((f) => f.op === "network").length, RS.MAX_NET);
});
