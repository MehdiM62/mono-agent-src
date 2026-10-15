/**
 * MonoAgent Bridge — watch_tab, read_console, read_network (plan C1)
 *
 * The coder's debug loop: change the code, reload, read what the page said.
 * watch_tab turns on the debugger's Runtime, Log and Network domains for a
 * tab and buffers what they report; read_console / read_network page
 * through those buffers with a cursor.
 *
 * Buffers, per tab: a ring of RING entries each for console and network,
 * every entry stamped with a seq that only grows (one counter per ring).
 * `since` is the last seq a reader has seen and `next` is what to pass the
 * next time. They live in this service worker's memory and nowhere else: a
 * restart of the worker (the extension reloading, Chrome reclaiming it)
 * clears them, and the next read says the tab is not watched. A `since`
 * beyond the newest seq is a cursor from before such a restart and reads
 * from the start.
 *
 * What is recorded of the network is the request line and the outcome:
 * method, URL, status, type, timing and size. Never a header, never a body,
 * so cookies and tokens in them never reach the buffer or the bridge.
 *
 * A request enters the network ring when it ends (finished, failed, or
 * redirected on to another URL), so an entry is complete when it is read
 * and a cursor never skips an update. One that never ends — an
 * event stream, a hung fetch — is not listed.
 *
 * The watch HOLDS its tab (agent_cdp.js), so the debugger stays attached
 * while nothing is being sent. Cancel on Chrome's debugging banner stops the
 * watch and keeps what was buffered; closing the tab drops it.
 *
 * Node-tested against fake debugger events (agent_watch.test.mjs).
 */

(function (root) {
  "use strict";

  const RING = 500;
  const PENDING_MAX = 500;
  const DEFAULT_LIMIT = 100;
  const MAX_TEXT = 4000;
  const MAX_URL = 2048;
  const MAX_FRAMES = 10;
  const LEVELS = ["debug", "log", "info", "warning", "error"];
  // Runtime.consoleAPICalled's type → level. Anything unlisted is "log".
  const CONSOLE_LEVELS = { debug: "debug", info: "info", warning: "warning", error: "error", assert: "error" };
  // Log.entryAdded's level → level.
  const LOG_LEVELS = { verbose: "debug", info: "info", warning: "warning", error: "error" };

  const tabs = new Map(); // tabId -> state

  function stateFor(tabId) {
    let st = tabs.get(tabId);
    if (!st) {
      st = { watching: false, console: [], network: [], consoleSeq: 0, networkSeq: 0, pending: new Map(), cutoff: 0 };
      tabs.set(tabId, st);
    }
    return st;
  }

  function cap(s, n) {
    s = String(s == null ? "" : s);
    return s.length > n ? s.slice(0, n) + "… [truncated]" : s;
  }

  const int = (n) => (Number.isFinite(n) ? Math.round(n) : 0);

  /** watch answers watch_tab. */
  async function watch(params) {
    const tabId = root.MonoAgentCdp.tabOf(params, "watch_tab");
    const on = params.on;
    if (on !== true && on !== false) throw new Error("watch_tab: on must be true or false");
    if (!on) {
      const st = tabs.get(tabId);
      if (st) st.watching = false;
      root.MonoAgentCdp.release(tabId, "watch");
      return { watching: false };
    }
    const st = stateFor(tabId);
    // Runtime.enable on a fresh debugger session replays the page's earlier
    // console messages. Those already buffered are dropped by their time.
    const last = st.console[st.console.length - 1];
    st.cutoff = last ? last.ts : 0;
    // Watching before the enables: their replay arrives before their replies.
    st.watching = true;
    root.MonoAgentCdp.hold(tabId, "watch");
    try {
      for (const domain of ["Runtime", "Log", "Network"]) {
        await root.MonoAgentCdp.send(tabId, `${domain}.enable`, {});
      }
    } catch (err) {
      st.watching = false;
      root.MonoAgentCdp.release(tabId, "watch");
      throw new Error(`watch_tab: ${err.message}`);
    }
    return { watching: true };
  }

  function push(st, ring, entry) {
    const key = ring === "console" ? "consoleSeq" : "networkSeq";
    st[key] += 1;
    const list = st[ring];
    list.push(Object.assign({ seq: st[key] }, entry));
    if (list.length > RING) list.shift();
  }

  // ── console ─────────────────────────────────────────────────────────────

  /** remoteText is how DevTools would print one console argument, roughly. */
  function remoteText(o) {
    if (!o) return "";
    if (o.type === "string") return String(o.value);
    if (o.unserializableValue !== undefined) return String(o.unserializableValue);
    if (o.type === "undefined") return "undefined";
    if (o.subtype === "null") return "null";
    if (o.type === "number" || o.type === "boolean" || o.type === "bigint") return String(o.value);
    if (o.preview && o.type === "object" && o.subtype !== "error") return previewText(o.preview);
    return String(o.description || o.className || o.type || "");
  }

  function previewText(p) {
    const items = (p.properties || []).map((q) => {
      const v = q.type === "string" ? JSON.stringify(q.value) : q.value === undefined ? q.type : q.value;
      return p.subtype === "array" ? String(v) : `${q.name}: ${v}`;
    });
    const body = items.join(", ") + (p.overflow ? ", …" : "");
    if (p.subtype === "array") return `[${body}]`;
    const name = p.description && p.description !== "Object" ? `${p.description} ` : "";
    return `${name}{${body}}`;
  }

  /** consoleText applies %s/%d/%i/%f/%o/%O/%c the way console.log does, then joins the rest. */
  function consoleText(args) {
    const list = Array.isArray(args) ? args.slice() : [];
    if (!list.length) return "";
    const first = list[0];
    if (first.type !== "string" || !/%[sdifoOc]/.test(first.value)) return list.map(remoteText).join(" ");
    list.shift();
    let out = String(first.value).replace(/%([sdifoOc%])/g, (m, f) => {
      if (f === "%") return "%";
      if (!list.length) return m;
      const a = list.shift();
      if (f === "c") return "";
      if (f === "d" || f === "i") return String(Math.trunc(Number(a.value)));
      if (f === "f") return String(Number(a.value));
      return remoteText(a);
    });
    if (list.length) out += " " + list.map(remoteText).join(" ");
    return out;
  }

  function stackText(trace) {
    const frames = (trace && trace.callFrames) || [];
    return frames
      .slice(0, MAX_FRAMES)
      .map((f) => `    at ${f.functionName || "<anonymous>"} (${f.url}:${f.lineNumber + 1}:${f.columnNumber + 1})`)
      .join("\n");
  }

  /** where reads a 0-based CDP location into the 1-based url/line/column of an entry. */
  function where(url, line, column) {
    return {
      url: cap(url || "", MAX_URL),
      line: Number.isInteger(line) ? line + 1 : 0,
      column: Number.isInteger(column) ? column + 1 : 0,
    };
  }

  function consoleEntry(method, p) {
    if (method === "Runtime.consoleAPICalled") {
      const level = CONSOLE_LEVELS[p.type] || "log";
      const top = (p.stackTrace && p.stackTrace.callFrames && p.stackTrace.callFrames[0]) || {};
      const e = Object.assign({ ts: int(p.timestamp), level, text: cap(consoleText(p.args), MAX_TEXT) }, where(top.url, top.lineNumber, top.columnNumber));
      if (level === "error" || level === "warning" || p.type === "trace") {
        const stack = stackText(p.stackTrace);
        if (stack) e.stack = stack;
      }
      return e;
    }
    if (method === "Runtime.exceptionThrown") {
      const d = p.exceptionDetails || {};
      const description = (d.exception && d.exception.description) || "";
      const text = [d.text || "", description.split("\n")[0]].filter(Boolean).join(" ");
      const e = Object.assign({ ts: int(p.timestamp), level: "error", text: cap(text, MAX_TEXT) }, where(d.url, d.lineNumber, d.columnNumber));
      const stack = stackText(d.stackTrace) || description.split("\n").slice(1, MAX_FRAMES + 1).join("\n");
      if (stack) e.stack = stack;
      return e;
    }
    if (method === "Log.entryAdded") {
      const l = p.entry || {};
      const e = Object.assign({ ts: int(l.timestamp), level: LOG_LEVELS[l.level] || "info", text: cap(l.text, MAX_TEXT) }, where(l.url, l.lineNumber));
      const stack = stackText(l.stackTrace);
      if (stack) e.stack = stack;
      return e;
    }
    return null;
  }

  // ── network ─────────────────────────────────────────────────────────────

  function finish(st, requestId, end, outcome) {
    const r = st.pending.get(requestId);
    if (!r) return;
    st.pending.delete(requestId);
    push(st, "network", {
      ts: r.ts,
      requestId,
      method: r.method,
      url: r.url,
      status: outcome.status !== undefined ? outcome.status : r.status,
      statusText: outcome.statusText !== undefined ? outcome.statusText : r.statusText,
      mimeType: outcome.mimeType !== undefined ? outcome.mimeType : r.mimeType,
      type: r.type,
      failed: !!outcome.failed,
      errorText: outcome.errorText || "",
      durationMs: Number.isFinite(end) && Number.isFinite(r.start) ? Math.max(0, Math.round((end - r.start) * 1000)) : 0,
      size: int(outcome.size),
    });
  }

  function networkEvent(st, method, p) {
    const id = p.requestId;
    if (!id) return;
    if (method === "Network.requestWillBeSent") {
      // The same requestId again, with the response that redirected it:
      // that hop ends here and the next one starts.
      if (p.redirectResponse) {
        const rr = p.redirectResponse;
        finish(st, id, p.timestamp, { status: int(rr.status), statusText: rr.statusText || "", mimeType: rr.mimeType || "", size: rr.encodedDataLength });
      }
      const req = p.request || {};
      st.pending.set(id, {
        ts: Number.isFinite(p.wallTime) ? Math.round(p.wallTime * 1000) : Date.now(),
        start: p.timestamp,
        method: req.method || "",
        url: cap(req.url || "", MAX_URL),
        type: p.type || "",
        status: 0,
        statusText: "",
        mimeType: "",
      });
      if (st.pending.size > PENDING_MAX) st.pending.delete(st.pending.keys().next().value);
      return;
    }
    const r = st.pending.get(id);
    if (!r) return;
    if (method === "Network.responseReceived") {
      const res = p.response || {};
      r.status = int(res.status);
      r.statusText = res.statusText || "";
      r.mimeType = res.mimeType || "";
      if (p.type) r.type = p.type;
    } else if (method === "Network.loadingFinished") {
      finish(st, id, p.timestamp, { size: p.encodedDataLength });
    } else if (method === "Network.loadingFailed") {
      finish(st, id, p.timestamp, { failed: true, errorText: p.errorText || p.blockedReason || (p.canceled ? "canceled" : "failed") });
    }
  }

  function onEvent(tabId, method, params) {
    const st = tabs.get(tabId);
    if (!st || !st.watching) return;
    if (method.startsWith("Network.")) {
      networkEvent(st, method, params);
      return;
    }
    const entry = consoleEntry(method, params);
    if (!entry) return;
    if (st.cutoff && entry.ts && entry.ts <= st.cutoff) return; // a replay of what is buffered
    push(st, "console", entry);
  }

  function onLost(tabId, reason) {
    if (reason === "target_closed") {
      tabs.delete(tabId);
      return;
    }
    const st = tabs.get(tabId);
    if (st) {
      st.watching = false;
      st.pending.clear();
    }
  }

  // ── reading ─────────────────────────────────────────────────────────────

  function watched(params, type) {
    const tabId = root.MonoAgentCdp.tabOf(params, type);
    const st = tabs.get(tabId);
    if (!st) {
      throw new Error(`${type}: tab ${tabId} is not watched: send watch_tab first (a restart of the extension's worker clears what was buffered)`);
    }
    return st;
  }

  function limitOf(limit, type) {
    if (limit === undefined || limit === null) return DEFAULT_LIMIT;
    if (!Number.isInteger(limit) || limit < 1 || limit > RING) throw new Error(`${type}: limit must be 1 to ${RING}`);
    return limit;
  }

  /** page reads entries after `since`, at most `limit` that `keep` accepts. */
  function page(list, newest, since, limit, keep) {
    let from = Number.isFinite(since) && since > 0 ? since : 0;
    if (from > newest) from = 0; // a cursor from before a worker restart
    const entries = [];
    let next = from;
    for (const e of list) {
      if (e.seq <= from) continue;
      next = e.seq;
      if (!keep(e)) continue;
      entries.push(Object.assign({}, e));
      if (entries.length >= limit) break;
    }
    return { entries, next };
  }

  function readConsole(params) {
    const st = watched(params, "read_console");
    const limit = limitOf(params.limit, "read_console");
    let levels = params.levels;
    if (typeof levels === "string") levels = levels.split(",").map((s) => s.trim()).filter(Boolean);
    if (levels !== undefined && levels !== null && !Array.isArray(levels)) throw new Error("read_console: levels must be a list");
    const want = levels && levels.length ? new Set(levels) : null;
    if (want) {
      for (const l of want) if (!LEVELS.includes(l)) throw new Error(`read_console: unknown level ${l} (${LEVELS.join(", ")})`);
    }
    return page(st.console, st.consoleSeq, params.since, limit, (e) => !want || want.has(e.level));
  }

  /** readNetwork answers read_network; failedOnly keeps failed requests and HTTP 4xx/5xx. */
  function readNetwork(params) {
    const st = watched(params, "read_network");
    const limit = limitOf(params.limit, "read_network");
    const failedOnly = params.failedOnly === true;
    return page(st.network, st.networkSeq, params.since, limit, (e) => !failedOnly || e.failed || e.status >= 400);
  }

  root.MonoAgentCdp.onEvent(onEvent);
  root.MonoAgentCdp.onLost(onLost);

  root.MonoAgentWatch = { watch, readConsole, readNetwork, consoleText, RING };
})(globalThis);
