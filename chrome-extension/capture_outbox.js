/**
 * MonoAgent Bridge — the offline capture queue (CLIP-08)
 *
 * Nothing captured is ever silently lost: a capture taken while the bridge
 * is down is queued in chrome.storage.local and flushed when it is back.
 * Loaded after capture.js, whose MonoCapture it adds queueCapture and
 * flushQueue to.
 */

(function (root) {
  "use strict";

  const MB = 1024 * 1024;
  const mb = (bytes) => `${(bytes / MB).toFixed(1)}MB`;
  const utf8Bytes = (text) => root.MonoCapture.utf8Bytes(text);

  const QUEUE_KEY = "captureQueue";
  const QUEUE_MAX_ENTRIES = 20;
  // chrome.storage.local is a ~10MB bucket; a big MHTML does not belong in it.
  const QUEUE_MAX_ENTRY_BYTES = 4 * MB;

  async function queueCapture(storage, envelope) {
    const size = utf8Bytes(JSON.stringify(envelope));
    if (size > QUEUE_MAX_ENTRY_BYTES) {
      return { queued: false, reason: `capture too large to queue offline (${mb(size)})` };
    }
    const current = (await storage.get(QUEUE_KEY))[QUEUE_KEY] || [];
    const next = current.concat([{ queuedAt: new Date().toISOString(), envelope }]).slice(-QUEUE_MAX_ENTRIES);
    await storage.set({ [QUEUE_KEY]: next });
    return { queued: true, depth: next.length };
  }

  /**
   * flushQueue sends what was queued, oldest first. `prepare(envelope)`, when
   * given, files an envelope that names no profile (D2: there is no
   * Unassigned pile) and answers null for one that has no profile to go to:
   * that one stays queued, as it was, until a later flush can file it.
   */
  async function flushQueue(storage, send, prepare) {
    const queued = (await storage.get(QUEUE_KEY))[QUEUE_KEY] || [];
    if (!queued.length) return { flushed: 0 };

    // The queue is emptied AFTER the sending, never before. Clearing it up
    // front and relying on the catch to put the rest back cannot work:
    // ws.onopen calls this and an MV3 socket is routinely torn down seconds
    // later, so the usual shape of a failed flush is the first envelope
    // going out and the rest being dropped into a queue key that was already
    // emptied — captures gone, and reported as flushed.
    //
    // The cost of this order is that a worker suspended between the last
    // send and the write-back re-sends what it already sent. A duplicate
    // capture is a duplicate row on the Go side; a lost one is a page the
    // person never gets back. CLIP-08 picks the duplicate every time.
    let flushed = 0;
    let held = 0;
    let reason = null;
    const sent = new Set();
    for (const entry of queued) {
      const envelope = prepare ? await prepare(entry.envelope) : entry.envelope;
      if (!envelope) {
        held++;
        continue;
      }
      try {
        send(envelope);
        flushed++;
        sent.add(entry);
      } catch (err) {
        reason = err.message;
        break;
      }
    }
    // Written back from what is there now, less what went: prepare may have
    // waited on the bridge, and a capture queued meanwhile must stay.
    const now = sent.size ? (await storage.get(QUEUE_KEY))[QUEUE_KEY] || [] : null;
    if (now) {
      const gone = new Set([...sent].map(entryKey));
      await storage.set({ [QUEUE_KEY]: now.filter((e) => !gone.has(entryKey(e))) });
    }
    const out = { flushed };
    if (held) out.held = held;
    if (reason) out.reason = reason;
    return out;
  }

  const entryKey = (e) => (e && e.envelope && e.envelope.id) || (e && e.queuedAt) || "";

  root.MonoCapture.queueCapture = queueCapture;
  root.MonoCapture.flushQueue = flushQueue;
})(globalThis);
