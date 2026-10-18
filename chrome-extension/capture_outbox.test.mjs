// Tests for the offline capture queue (CLIP-08): what is queued while the
// bridge is down, and how a flush sends it.
// `node --test 'chrome-extension/*.test.mjs'`.

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const { MonoCapture } = loadExtensionScripts(["capture_meta.js", "capture.js", "capture_outbox.js"]);

test("a capture taken with the bridge down is queued, then flushed in order", async () => {
  const store = {};
  const storage = {
    get: async (key) => ({ [key]: store[key] }),
    set: async (update) => Object.assign(store, update),
  };

  assert.deepEqual(await MonoCapture.queueCapture(storage, { id: "a" }), { queued: true, depth: 1 });
  assert.deepEqual(await MonoCapture.queueCapture(storage, { id: "b" }), { queued: true, depth: 2 });

  const sent = [];
  assert.deepEqual(await MonoCapture.flushQueue(storage, (e) => sent.push(e.id)), { flushed: 2 });
  assert.deepEqual(sent, ["a", "b"]);
  assert.deepEqual(await MonoCapture.flushQueue(storage, () => {}), { flushed: 0 }, "the queue is drained, not replayed");
});

test("a flush stops at the first frame that does not reach the wire", async () => {
  const store = {};
  const storage = {
    get: async (key) => ({ [key]: store[key] }),
    set: async (update) => Object.assign(store, update),
  };
  await MonoCapture.queueCapture(storage, { id: "a" });
  await MonoCapture.queueCapture(storage, { id: "b" });
  await MonoCapture.queueCapture(storage, { id: "c" });

  const sent = [];
  const result = await MonoCapture.flushQueue(storage, (envelope) => {
    if (sent.length >= 1) throw new Error("the bridge disconnected mid-send");
    sent.push(envelope.id);
  });

  assert.equal(result.flushed, 1);
  assert.deepEqual(sent, ["a"]);
  // Emptying the queue before sending means this recovery can never run: the
  // entries are already gone by the time the send fails.
  assert.deepEqual(store.captureQueue.map((e) => e.envelope.id), ["b", "c"]);
});

test("the queue's size cap counts bytes on the wire, not UTF-16 units", async () => {
  const storage = { get: async () => ({}), set: async () => {} };
  // 2M CJK characters: 2M UTF-16 units — comfortably under the 4MB cap by
  // `.length` — and 6MB of UTF-8, which is what chrome.storage.local's
  // byte-counted bucket actually sees.
  const envelope = { id: "cjk", meta: {}, artifacts: [{ name: "readable.md", bytes: "\u6f22".repeat(2 * 1024 * 1024) }] };
  const result = await MonoCapture.queueCapture(storage, envelope);
  assert.equal(result.queued, false, "6MB is over the 4MB queue cap however you spell it");
  assert.match(result.reason, /too large to queue offline/);
});

test("a capture too big for the storage bucket is refused rather than half-written", async () => {
  const storage = { get: async () => ({}), set: async () => { throw new Error("QUOTA_BYTES exceeded"); } };
  const huge = { id: "x", data: { bytes: "A".repeat(5 * 1024 * 1024) } };
  const result = await MonoCapture.queueCapture(storage, huge);
  assert.equal(result.queued, false);
  assert.match(result.reason, /too large to queue offline/);
});

// D2 (#822): the flush files what names no profile (prepare), keeps what has
// nowhere to go (prepare answers null) queued and unsent, and never loses a
// capture queued while it ran.
test("a flush files captures as it sends them, and holds those with no profile to go to", async () => {
  const store = {};
  const storage = {
    get: async (key) => ({ [key]: store[key] }),
    set: async (update) => Object.assign(store, update),
  };
  await MonoCapture.queueCapture(storage, { id: "a", meta: {} });
  await MonoCapture.queueCapture(storage, { id: "b", meta: { profile: "p-home" } });
  await MonoCapture.queueCapture(storage, { id: "c", meta: {} });

  const sent = [];
  const prepare = async (e) => {
    if (e.id === "a") await MonoCapture.queueCapture(storage, { id: "d", meta: {} }); // captured mid-flush
    if (e.id === "c") return null; // nowhere to file it yet
    return e.meta.profile ? e : Object.assign({}, e, { meta: { profile: "p-work" } });
  };
  const result = await MonoCapture.flushQueue(storage, (e) => sent.push([e.id, e.meta.profile]), prepare);
  assert.deepEqual(sent, [["a", "p-work"], ["b", "p-home"]]);
  assert.equal(result.flushed, 2);
  assert.equal(result.held, 1);
  assert.deepEqual(store.captureQueue.map((e) => e.envelope.id), ["c", "d"], "held and new captures stay");
  assert.equal(store.captureQueue[0].envelope.meta.profile, undefined, "a held capture is kept as it was");
});
