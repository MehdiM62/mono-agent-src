// The pairing-without-a-step state machine (autopair.js): what the worker
// does for each thing the bridge can say.
//
// `node --test chrome-extension/autopair.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const { MonoAutoPair: A } = loadExtensionScripts(["autopair.js"]);

const json = (body, status = 200) => ({ ok: status >= 200 && status < 300, status, json: async () => body });

/** bridge answers /status and /pair/claim on the given ports; any other port is refused. */
function fakeBridge({ ports = [9222], doc = {}, claim = json({ token: "t".repeat(64), id: "abc123" }) } = {}) {
  const calls = [];
  const impl = async (url, init = {}) => {
    calls.push({ url, method: init.method || "GET", body: init.body });
    const m = /^http:\/\/127\.0\.0\.1:(\d+)(\/.*)$/.exec(url);
    if (!m || !ports.includes(Number(m[1]))) throw new TypeError("refused");
    if (m[2] === "/monoagent/status") return json(Object.assign({ service: "monoagent-bridge", signedIn: false, paired: false, autoPair: true }, doc));
    if (m[2] === "/monoagent/pair/claim") return claim;
    throw new Error("unexpected " + url);
  };
  return { impl, calls };
}

test("bridge up and signed in: claims, and sends who it is", async () => {
  const b = fakeBridge({ doc: { signedIn: true } });
  const r = await A.step({ fetch: b.impl, instance: "0123456789abcdef", label: "Edge 0123" });
  assert.equal(r.kind, "claimed");
  assert.equal(r.token.length, 64);
  assert.equal(r.wsUrl, "ws://127.0.0.1:9222/monoagent");
  const claim = b.calls.find((c) => c.method === "POST");
  assert.deepEqual(JSON.parse(claim.body), { instance: "0123456789abcdef", label: "Edge 0123" });
});

test("bridge up and signed out: no claim, says signed_out", async () => {
  const b = fakeBridge({ doc: { signedIn: false } });
  const r = await A.step({ fetch: b.impl });
  assert.equal(r.kind, "signed_out");
  assert.equal(b.calls.some((c) => c.method === "POST"), false);
});

test("no bridge on any port: down", async () => {
  const b = fakeBridge({ ports: [] });
  assert.equal((await A.step({ fetch: b.impl })).kind, "down");
});

test("the fallback port is found", async () => {
  const b = fakeBridge({ ports: [9323], doc: { signedIn: true } });
  const r = await A.step({ fetch: b.impl });
  assert.equal(r.kind, "claimed");
  assert.equal(r.wsUrl, "ws://127.0.0.1:9323/monoagent");
});

test("something else on the port is not the bridge", async () => {
  const impl = async () => json({ Browser: "Chrome/140", signedIn: true });
  assert.equal((await A.step({ fetch: impl })).kind, "down");
});

test("automatic pairing off: manual path, no claim", async () => {
  const b = fakeBridge({ doc: { signedIn: true, autoPair: false } });
  const r = await A.step({ fetch: b.impl });
  assert.equal(r.kind, "disabled");
  assert.equal(b.calls.some((c) => c.method === "POST"), false);
});

test("a refused claim carries the bridge's reason; a mute one is refused too", async () => {
  const b = fakeBridge({ doc: { signedIn: true }, claim: json({ error: "too many pairing attempts" }, 429) });
  const r = await A.step({ fetch: b.impl });
  assert.equal(r.kind, "refused");
  assert.match(r.error, /too many/);
  const odd = fakeBridge({ doc: { signedIn: true }, claim: json({}, 200) });
  assert.equal((await A.step({ fetch: odd.impl })).kind, "refused");
});

test("a bridge that accepts and never answers does not hang the step", async () => {
  const impl = (url, init = {}) =>
    new Promise((_, reject) => {
      init.signal && init.signal.addEventListener("abort", () => reject(new Error("aborted")));
    });
  const r = await A.step({ fetch: impl, timeoutMs: 30 });
  assert.equal(r.kind, "down");
});

test("the configured address is asked first", () => {
  assert.deepEqual(A.bases({ wsUrl: "ws://127.0.0.1:9500/monoagent" }), ["http://127.0.0.1:9500", "http://127.0.0.1:9222", "http://127.0.0.1:9323"]);
});
