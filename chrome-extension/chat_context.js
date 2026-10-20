/**
 * MonoAgent Bridge — chat page context (issue #910, split from chat_core.js)
 *
 * What the chat may say about the page the person is looking at: the caps,
 * the URL redaction, the caption transcript and the message wrapper. Pure;
 * chat_core.js re-exports every name, so no caller changes. Load before
 * chat_core.js. Runs in the panel, the worker and node; touches no DOM.
 */

(function (root) {
  "use strict";

  const TEXT_CAP = 20000; // page text sent with a message (UTF-8 bytes, like every cap but KEEP_*)
  const SELECTION_CAP = 8000;
  const TRANSCRIPT_CAP = 24 * 1024; // captions of a video page (bytes, the server's own cap)
  const DESCRIPTION_CAP = 4 * 1024;
  const CHANNEL_CAP = 256;
  const TITLE_CAP = 300;
  const URL_CAP = 2000;

  const str = (v) => (typeof v === "string" ? v : "");

  // ── What is sent about the page ────────────────────────────────────

  /**
   * buildContext caps whatever the page produced. It is applied again in
   * the worker, so a panel (or a page) cannot make a message larger than
   * this. Returns null for no usable page at all.
   */
  function buildContext(raw) {
    if (!raw || typeof raw !== "object") return null;
    const url = redactUrl(raw.url);
    if (!url) return null;
    // A local file's address and title are named; its text never leaves the
    // machine's browser (it is usually the person's own notes and documents).
    const local = /^file:/i.test(url);
    const out = {
      url,
      title: clipBytes(raw.title, TITLE_CAP).trim(),
      text: local ? "" : clipBytes(raw.text, TEXT_CAP),
      selection: local ? "" : clipBytes(raw.selection, SELECTION_CAP),
    };
    // A video page: the captions (when there are any) and the video's own
    // details ride along; "video" tells the server the captions were tried.
    if (raw.video === true && !local) {
      out.video = true;
      const id = str(raw.video_id);
      if (/^[A-Za-z0-9_-]{11}$/.test(id)) out.video_id = id;
      out.channel = clipBytes(raw.channel, CHANNEL_CAP).trim();
      out.description = clipBytes(raw.description, DESCRIPTION_CAP);
      out.transcript = clipBytes(raw.transcript, TRANSCRIPT_CAP);
    }
    return out;
  }

  const utf8Bytes = (s) => new TextEncoder().encode(s).length;

  /**
   * buildTranscript turns caption cues ({start, text}) into plain text, no
   * timestamps. Up to TRANSCRIPT_CAP bytes it is all of it. Longer: the first
   * half of the budget is the beginning, in full; the rest of the budget is
   * cues sampled evenly over the remainder of the video (so the middle and
   * the end are represented), in order, after an ellipsis.
   */
  function buildTranscript(cues, cap) {
    const limit = cap || TRANSCRIPT_CAP;
    const lines = (Array.isArray(cues) ? cues : [])
      .map((c) => str(c && c.text).replace(/\s+/g, " ").trim())
      .filter(Boolean);
    const all = lines.join(" ");
    if (utf8Bytes(all) <= limit) return all;
    const head = [];
    let used = 0;
    let i = 0;
    for (; i < lines.length; i++) {
      const n = utf8Bytes(lines[i]) + 1;
      if (used + n > Math.floor(limit / 2)) break;
      head.push(lines[i]);
      used += n;
    }
    const rest = lines.slice(i);
    const average = rest.reduce((sum, l) => sum + utf8Bytes(l) + 1, 0) / (rest.length || 1);
    const take = rest.length ? Math.min(rest.length, Math.floor((limit - used - 4) / average)) : 0;
    const sampled = [];
    for (let j = 0; j < take; j++) sampled.push(rest[Math.floor((j * rest.length) / take)]);
    return clipBytes(head.join(" ") + (sampled.length ? " … " + sampled.join(" ") : ""), limit);
  }

  /** utf8Length is how many bytes of UTF-8 a code point takes. */
  const utf8Length = (cp) => (cp < 0x80 ? 1 : cp < 0x800 ? 2 : cp < 0x10000 ? 3 : 4);

  /**
   * clipBytes cuts to at most `n` UTF-8 bytes at a character boundary. The
   * server counts bytes, so counting UTF-16 units here would let a CJK page
   * through the client and fail on the server.
   */
  function clipBytes(s, n) {
    const text = str(s);
    if (text.length <= n / 4) return text; // cannot exceed n bytes
    let bytes = 0;
    let end = 0;
    for (const ch of text) {
      const size = utf8Length(ch.codePointAt(0));
      if (bytes + size > n) break;
      bytes += size;
      end += ch.length;
    }
    return text.slice(0, end);
  }

  /**
   * stripUrl keeps scheme, host and path of a web or file address: the query
   * and fragment of a real page carry tokens, session ids and search terms,
   * and credentials in the address are never sent. "" for anything else.
   */
  function stripUrl(raw) {
    let u;
    try {
      u = new URL(clipBytes(raw, URL_CAP));
    } catch {
      return "";
    }
    if (!/^(https?|file):$/.test(u.protocol)) return "";
    if (u.protocol !== "file:" && !u.hostname) return "";
    return `${u.protocol}//${u.protocol === "file:" ? "" : u.host}${u.pathname}`;
  }

  // The same rules, vector for vector, as redactURL in
  // internal/extension/chat_context.go (url_redaction.golden.json).
  const KEEP_NAMES = new Set(["v", "list", "index", "t", "q", "query", "search_query", "p", "page", "id", "tab", "sort", "lang", "hl"]);
  const SENSITIVE_NAMES = [
    "token", "code", "key", "secret", "pass", "pwd", "auth", "session", "sid", "sig", "signature",
    "credential", "otp", "nonce", "state", "csrf", "reset", "verify", "magic", "ticket", "jwt",
    "bearer", "hmac", "expires", "x-amz", "x-goog", "access", "refresh", "id_token", "api", "apikey",
  ];
  const URL_REDACTED = "REDACTED";
  const URL_MAX_BYTES = 1024;

  /** urlDecode reads a query component like the Go side: bad escapes stay as written. */
  function urlDecode(s) {
    try {
      return decodeURIComponent(s.replace(/\+/g, " "));
    } catch {
      return s;
    }
  }
  const sensitiveName = (lower) => SENSITIVE_NAMES.some((n) => lower.includes(n));
  const opaqueSecret = (v) => /^[A-Za-z0-9+/=_-]{20,}$/.test(v);

  /** Decodes a name until it stops changing (three times at most): a double-encoded name cannot dodge the denylist. */
  function urlDecodeFully(s) {
    for (let i = 0; i < 3; i++) {
      const d = urlDecode(s);
      if (d === s) break;
      s = d;
    }
    return s;
  }
  const KEPT_VALUE_MAX = 64; // longest value kept under an allowlisted name
  const RAW_URL_MAX = 32 * 1024; // longest address redacted whole

  /**
   * Parameters are separated by & or ;. A value is kept only when it is short
   * and not opaque: at most 64 characters under an allowlisted name (and a
   * playlist id may look opaque), at most 16 under any other name that is not
   * sensitive.
   */
  function redactQuery(search) {
    let out = "";
    let sep = "&";
    const rq = search.replace(/^\?/, "");
    let start = 0;
    for (let i = 0; i <= rq.length; i++) {
      if (i < rq.length && rq[i] !== "&" && rq[i] !== ";") continue;
      const pair = rq.slice(start, i);
      const thisSep = sep;
      start = i + 1;
      if (i < rq.length) sep = rq[i];
      if (!pair) continue;
      if (out) out += thisSep;
      const eq = pair.indexOf("=");
      const rawName = eq < 0 ? pair : pair.slice(0, eq);
      const name = urlDecodeFully(rawName);
      const lower = name.toLowerCase();
      if (eq < 0) {
        out += sensitiveName(lower) || opaqueSecret(name) ? URL_REDACTED : rawName;
        continue;
      }
      const rawVal = pair.slice(eq + 1);
      const val = urlDecodeFully(rawVal);
      const limit = KEEP_NAMES.has(lower) ? KEPT_VALUE_MAX : sensitiveName(lower) ? -1 : 16;
      const keep = limit >= 0 && [...val].length <= limit && (lower === "list" || !opaqueSecret(val));
      out += rawName + "=" + (keep ? rawVal : URL_REDACTED);
    }
    return out;
  }

  /** A path segment of 20+ url-safe characters with a digit, a letter and at most one - or _ (a reset link, not a slug). */
  function pathSecret(seg) {
    if (!/^[A-Za-z0-9_-]{20,}$/.test(seg)) return false;
    return /\d/.test(seg) && /[A-Za-z]/.test(seg) && (seg.match(/[-_]/g) || []).length < 2;
  }

  /** A fragment that starts like a token header or has three long dot-separated parts. */
  function jwtLike(frag) {
    return frag.startsWith("eyJ") || frag.split(".").filter((p) => p.length >= 8).length >= 3;
  }

  /**
   * redactUrl is the address the model gets: scheme, host, path and query,
   * where a query value or path segment that may be a secret is REDACTED;
   * user-info is dropped; the fragment survives only as a plain anchor; a
   * file address keeps its path only; at most 1 KiB, cut AFTER redacting (a
   * cut inside a secret would leave a harmless-looking prefix of it). "" for
   * anything that is not http(s) or file.
   */
  function redactUrl(raw) {
    let text = str(raw).trim();
    if (utf8Bytes(text) > RAW_URL_MAX) {
      // Too long to read whole: its query and fragment go.
      const i = text.search(/[?#]/);
      text = i >= 0 ? text.slice(0, i) : clipBytes(text, RAW_URL_MAX);
    }
    let u;
    try {
      u = new URL(text);
    } catch {
      return "";
    }
    if (!/^(https?|file):$/.test(u.protocol)) return "";
    const file = u.protocol === "file:";
    if (!file && !u.hostname) return "";
    const path = file ? u.pathname : u.pathname.split("/").map((seg) => (pathSecret(seg) ? URL_REDACTED : seg)).join("/");
    let out = `${u.protocol}//${file ? "" : u.host}${path}`;
    if (!file) {
      const q = redactQuery(u.search);
      if (q) out += "?" + q;
      if (/^#[A-Za-z0-9._-]{1,64}$/.test(u.hash) && !jwtLike(u.hash.slice(1))) out += u.hash;
    }
    return clipBytes(out, URL_MAX_BYTES);
  }

  // The wrapper the daemon puts in front of a message that carries a page
  // (withPageContext in internal/extension/chat_context.go). These strings
  // are spelled twice on purpose; chat_golden_test.go (Go) and
  // chat_golden.test.mjs read the same golden file and fail on any drift.
  const WRAPPER_INTRO = "The person is looking at a web page.";
  const MESSAGE_BOUNDARY = "[the person's message follows]";
  // Daemons before the boundary ended the wrapper with this line.
  const LEGACY_MARKER = "The person's message:\n";

  /**
   * unwrapMessage returns what the person typed from a message as the daemon
   * recorded it. A wrapped message starts with the intro; its message starts
   * after the boundary line. The FIRST boundary is the real one: a page
   * cannot write it (the server turns every bracket in page fields into a
   * parenthesis), a person can, and everything after the real one is theirs.
   * Older daemons: everything up to the last old marker. Anything else,
   * including text that is not wrapped at all, is shown as it is.
   */
  function unwrapMessage(text) {
    const t = str(text);
    if (!t.startsWith(WRAPPER_INTRO)) return t;
    const marker = MESSAGE_BOUNDARY + "\n";
    const at = t.indexOf(marker);
    if (at >= 0) return t.slice(at + marker.length);
    const old = t.lastIndexOf(LEGACY_MARKER);
    return old >= 0 ? t.slice(old + LEGACY_MARKER.length) : t;
  }

  /** The video pages the chat reads captions from (the same ones the video summary reads). */
  const VIDEO_PAGE = /^https?:\/\/(([^/]+\.)?youtube\.com\/(watch|shorts\/|live\/)|youtu\.be\/)/i;
  const isVideoPage = (url) => VIDEO_PAGE.test(str(url));

  /** pageLabel is what the chip says about the page a message was sent with. */
  function pageLabel(context) {
    if (!context) return "";
    return (context.title || context.url || "") + (context.transcript ? " (with captions)" : "");
  }

  root.MonoChatContext = {
    buildContext, stripUrl, clipBytes, redactUrl, unwrapMessage, buildTranscript, isVideoPage, pageLabel,
    TEXT_CAP, SELECTION_CAP, TRANSCRIPT_CAP, TITLE_CAP, WRAPPER_INTRO, MESSAGE_BOUNDARY,
  };
})(globalThis);
