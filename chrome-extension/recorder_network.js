/**
 * MonoAgent Bridge -- API calls the recorded page makes (section 8.2)
 *
 * While a recording runs, the page's fetch / XMLHttpRequest calls that are
 * answered with JSON are captured so `monoagentcli record api` can turn them
 * into http.request nodes. Two halves, one file:
 *
 *   hook(win, post)  runs in the page's MAIN world (recorder_wiring.js injects
 *                    it there): it wraps fetch and XHR and posts what it saw
 *                    to the window. The page's own calls are never changed,
 *                    delayed or failed by it -- every read is of a clone, and
 *                    every error inside is swallowed.
 *   relay(win, send) runs in the isolated world beside recorder.js: it takes
 *                    those posts and sends them to the worker as
 *                    recorder_net, only while this frame is being recorded.
 *
 * The page sees the posts (they carry its own requests back to it), and a
 * page could forge one; either way the worker applies the privacy rules
 * (recorder_privacy.js redactNet) before anything is queued, and Go applies
 * them again. What the hook keeps is already small: header names and values
 * as the page set them (never cookies -- page code cannot see those), the
 * request body when it is text, and a SAMPLE of the JSON response: arrays cut
 * to a few items, long strings shortened.
 *
 * hook() is idempotent per window and stopped with unhook(): the wrappers
 * stay (unwrapping could break a page that wrapped fetch after us) but go
 * quiet. Plain ASCII only (recorder_encoding.test.mjs).
 */

(function (root) {
  "use strict";

  const MSG = "__monoagentNet";
  const SAMPLE_MAX = 8192;
  const READ_MAX = 512 * 1024; // larger responses are not read for a sample
  const BODY_MAX = 64 * 1024;

  const isJson = (ct) => /(^|[/+])json(\s*;|$)/i.test(String(ct || "").trim());

  /** shrink cuts arrays to `items` entries and strings to `chars`. */
  function shrink(v, items, chars, depth) {
    if (depth > 12) return null;
    if (Array.isArray(v)) return v.slice(0, items).map((x) => shrink(x, items, chars, depth + 1));
    if (v && typeof v === "object") {
      const out = {};
      for (const k of Object.keys(v)) out[k] = shrink(v[k], items, chars, depth + 1);
      return out;
    }
    if (typeof v === "string" && v.length > chars) return v.slice(0, chars) + "...";
    return v;
  }

  /** sampleJson returns a small JSON text standing for `text`, or "". */
  function sampleJson(text) {
    let v;
    try {
      v = JSON.parse(String(text || ""));
    } catch {
      return "";
    }
    for (const [items, chars] of [[3, 200], [1, 60], [1, 20]]) {
      const s = JSON.stringify(shrink(v, items, chars, 0));
      if (s !== undefined && s.length <= SAMPLE_MAX) return s;
    }
    return "";
  }

  /** headerList turns Headers, [[k, v]] or {k: v} into [{name, value}]. */
  function headerList(h) {
    const out = [];
    if (!h) return out;
    try {
      if (typeof h.forEach === "function" && !Array.isArray(h)) {
        h.forEach((value, name) => out.push({ name: String(name), value: String(value) }));
      } else if (Array.isArray(h)) {
        for (const pair of h) if (pair && pair.length >= 2) out.push({ name: String(pair[0]), value: String(pair[1]) });
      } else if (typeof h === "object") {
        for (const k of Object.keys(h)) out.push({ name: k, value: String(h[k]) });
      }
    } catch {
      // a hostile Headers-like object
    }
    return out;
  }

  const headerValue = (list, name) => {
    const n = name.toLowerCase();
    const h = list.find((x) => x.name.toLowerCase() === n);
    return h ? h.value : "";
  };

  /** bodyText is a request body as text, when it is text. */
  function bodyText(body) {
    if (body == null) return "";
    if (typeof body === "string") return body.slice(0, BODY_MAX);
    try {
      if (typeof URLSearchParams === "function" && body instanceof URLSearchParams) return body.toString().slice(0, BODY_MAX);
    } catch {
      // cross-realm object
    }
    return ""; // FormData, Blob, streams, buffers: not recorded
  }

  function absolute(win, url) {
    try {
      const u = new URL(String(url), win.location && win.location.href);
      return /^https?:$/.test(u.protocol) ? u.toString() : "";
    } catch {
      return "";
    }
  }

  function hook(win, post) {
    const w = win || root;
    const send = post || ((entry) => w.postMessage({ [MSG]: true, entry }, "*"));
    if (w.__monoNetHook) {
      w.__monoNetHook.on = true;
      return w.__monoNetHook;
    }
    const state = { on: true };
    w.__monoNetHook = state;
    const now = () => Date.now();
    const emit = (entry) => {
      if (!state.on || !entry.url) return;
      try {
        send(entry);
      } catch {
        // nothing listening
      }
    };

    const origFetch = typeof w.fetch === "function" ? w.fetch : null;
    if (origFetch) {
      w.fetch = function (input, init) {
        const p = origFetch.apply(this, arguments);
        if (!state.on) return p;
        try {
          const req = input && typeof input === "object" && typeof input.url === "string" ? input : null;
          const headers = headerList((init && init.headers) || (req && req.headers));
          const base = {
            initiator: "fetch",
            method: String((init && init.method) || (req && req.method) || "GET"),
            url: absolute(w, req ? req.url : input),
            requestHeaders: headers,
            requestType: headerValue(headers, "content-type"),
            requestBody: bodyText(init && init.body),
          };
          const started = now();
          p.then(
            (res) => {
              try {
                const ct = (res.headers && res.headers.get("content-type")) || "";
                if (!isJson(ct)) return;
                const len = Number(res.headers.get("content-length") || 0);
                const entry = Object.assign({}, base, { t: started, status: res.status, contentType: ct });
                if (len > READ_MAX) return emit(entry);
                res
                  .clone()
                  .text()
                  .then((text) => emit(Object.assign(entry, { responseSample: text.length > READ_MAX ? "" : sampleJson(text) })))
                  .catch(() => emit(entry));
              } catch {
                // never disturb the page
              }
            },
            () => {}
          );
        } catch {
          // never disturb the page
        }
        return p;
      };
    }

    const XHR = w.XMLHttpRequest;
    if (XHR && XHR.prototype) {
      const proto = XHR.prototype;
      const origOpen = proto.open;
      const origSet = proto.setRequestHeader;
      const origSend = proto.send;
      proto.open = function (method, url) {
        try {
          this.__monoNet = { method: String(method || "GET"), url: absolute(w, url), headers: [] };
        } catch {
          // never disturb the page
        }
        return origOpen.apply(this, arguments);
      };
      proto.setRequestHeader = function (name, value) {
        try {
          if (this.__monoNet) this.__monoNet.headers.push({ name: String(name), value: String(value) });
        } catch {
          // never disturb the page
        }
        return origSet.apply(this, arguments);
      };
      proto.send = function (body) {
        try {
          const info = this.__monoNet;
          if (info && state.on) {
            const started = now();
            const xhr = this;
            const requestBody = bodyText(body);
            xhr.addEventListener("loadend", () => {
              try {
                const ct = xhr.getResponseHeader("content-type") || "";
                if (!isJson(ct) || !xhr.status) return;
                let text = "";
                if (xhr.responseType === "" || xhr.responseType === "text") text = xhr.responseText;
                else if (xhr.responseType === "json") text = JSON.stringify(xhr.response);
                emit({
                  initiator: "xhr", t: started, method: info.method, url: info.url, status: xhr.status, contentType: ct,
                  requestHeaders: info.headers, requestType: headerValue(info.headers, "content-type"), requestBody,
                  responseSample: text && text.length <= READ_MAX ? sampleJson(text) : "",
                });
              } catch {
                // never disturb the page
              }
            });
          }
        } catch {
          // never disturb the page
        }
        return origSend.apply(this, arguments);
      };
    }
    return state;
  }

  function unhook(win) {
    const w = win || root;
    if (w.__monoNetHook) w.__monoNetHook.on = false;
  }

  /** relay forwards the hook's posts to the worker while `active()` says so. */
  function relay(win, send, active) {
    const listener = (e) => {
      const d = e && e.data;
      if (!d || d[MSG] !== true || e.source !== win || !d.entry || typeof d.entry !== "object") return;
      if (active && !active()) return;
      send({ type: "recorder_net", entry: d.entry });
    };
    win.addEventListener("message", listener);
    return () => win.removeEventListener("message", listener);
  }

  root.MonoRecorderNet = { hook, unhook, relay, sampleJson, headerList, isJson, bodyText, MSG };

  // In the isolated world (the extension's runtime is there; the MAIN world
  // has none): relay once per document, while this frame's recorder runs.
  if (
    typeof window !== "undefined" &&
    typeof chrome !== "undefined" &&
    chrome.runtime &&
    chrome.runtime.id &&
    !root.__monoNetRelay
  ) {
    root.__monoNetRelay = relay(
      window,
      (msg) => chrome.runtime.sendMessage(msg).catch(() => {}),
      () => !!(root.__monoRecorderActive && !root.__monoRecorderActive.isStopped())
    );
  }
})(globalThis);
