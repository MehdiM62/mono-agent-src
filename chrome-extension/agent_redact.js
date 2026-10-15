/**
 * MonoAgent Bridge — secrets out of the URLs the coder's commands record
 *
 * A request URL or a script URL can carry a credential in its query: an
 * OAuth code or access_token, a signed URL's signature, an API key. The
 * console and network buffers (agent_watch.js) and the submit guard's
 * records (agent_submit_guard.js) cross the bridge to an agent, so they
 * keep a URL's shape and drop those values at record time: every query (or
 * fragment) parameter whose name contains one of SENSITIVE_NAMES keeps its
 * name and loses its value to "•••". Everything else stays as written, so a
 * debugging agent still sees ?page=2.
 *
 * SENSITIVE_NAMES is internal/extension/chat_context.go's urlSensitiveNames,
 * spelled the same (agent_redact.test.mjs compares the two).
 */

(function (root) {
  "use strict";

  const SENSITIVE_NAMES = [
    "token", "code", "key", "secret", "pass", "pwd", "auth", "session", "sid", "sig", "signature",
    "credential", "otp", "nonce", "state", "csrf", "reset", "verify", "magic", "ticket", "jwt",
    "bearer", "hmac", "expires", "x-amz", "x-goog", "access", "refresh", "id_token", "api", "apikey",
  ];
  const MASK = "•••";
  // A URL in free text (a console line, a stack frame) ends at a space or a quote.
  const URL_IN_TEXT = /\bhttps?:\/\/[^\s"'<>`]+/g;

  function decode(s) {
    try {
      return decodeURIComponent(s.replace(/\+/g, " "));
    } catch {
      return s;
    }
  }

  /** decodeFully undoes encoding until it stops changing: a double-encoded name cannot dodge the list. */
  function decodeFully(s) {
    for (let i = 0; i < 3; i++) {
      const d = decode(s);
      if (d === s) break;
      s = d;
    }
    return s;
  }

  const sensitive = (name) => {
    const lower = decodeFully(name).toLowerCase();
    return SENSITIVE_NAMES.some((n) => lower.includes(n));
  };

  /** params masks the sensitive values of one query or fragment, keeping its separators. */
  function params(q) {
    return q.replace(/([^&;=]+)=([^&;]*)/g, (all, name, value) => (value && sensitive(name) ? `${name}=${MASK}` : all));
  }

  /** url masks the sensitive query and fragment values of one URL. */
  function url(u) {
    const s = String(u == null ? "" : u);
    const hash = s.indexOf("#");
    const head = hash === -1 ? s : s.slice(0, hash);
    let frag = hash === -1 ? "" : s.slice(hash);
    const q = head.indexOf("?");
    const out = q === -1 ? head : head.slice(0, q + 1) + params(head.slice(q + 1));
    // An OAuth implicit grant puts the token in the fragment.
    if (frag.includes("=")) frag = "#" + params(frag.slice(1));
    return out + frag;
  }

  // What follows a URL in a stack frame, "…/a.js?v=1:3:9)", and is not its query.
  const FRAME_TAIL = /(?::\d+){1,2}\)*$|\)+$/;

  /** text masks every URL found in free text. */
  function text(t) {
    return String(t == null ? "" : t).replace(URL_IN_TEXT, (m) => {
      const tail = FRAME_TAIL.exec(m);
      const cut = tail ? m.length - tail[0].length : m.length;
      return url(m.slice(0, cut)) + m.slice(cut);
    });
  }

  root.MonoAgentRedact = { url, text, SENSITIVE_NAMES, MASK };
})(globalThis);
