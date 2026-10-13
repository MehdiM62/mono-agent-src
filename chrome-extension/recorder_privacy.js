/**
 * MonoAgent Bridge -- what the recorder must never send (spec section 8.2)
 *
 * One place for the privacy rules, loaded first into the page so every
 * other recorder script applies the same ones: typed values (recorder.js),
 * picked data (recorder_list.js) and DOM snippets (recorder.js snippetOf).
 *
 * A value is withheld when the FIELD says it is secret:
 *   - input type password or hidden;
 *   - autocomplete current-password / new-password / one-time-code / cc-*;
 *   - its name, id, label, placeholder or aria-label reads like a secret:
 *     cvv, cvc, csc, cvn, ssn, pin, otp, passcode, security code, social
 *     security, one-time / verification code;
 * or when the VALUE is a card number (Luhn-valid 13-19 digits).
 *
 * A field that was EVER type=password stays sensitive (markPassword), and so
 * does one drawn with -webkit-text-security (a CSS-masked "password").
 * sensitiveField is the field-level half, with no value involved: it sets
 * the fingerprint's `sensitive` flag, which Go also enforces.
 *
 * sanitizeUrl is applied to every recorded URL before it is queued, so the
 * offline outbox never holds a login code or a session token; Go applies
 * the same rules again.
 *
 * The page scripts must stay plain ASCII (recorder_encoding.test.mjs).
 */

(function (root) {
  "use strict";

  // Loaded once per page world. A second injection (the element picker
  // while a recording runs) must not replace it: the WeakSet of fields that
  // were ever passwords lives here.
  if (root.MonoRecorderPrivacy) return;

  const tagOf = (el) => String((el && el.tagName) || "").toLowerCase();
  const attr = (el, name) => (el && el.getAttribute ? el.getAttribute(name) : null);
  const typeOf = (el) => String(attr(el, "type") || "text").toLowerCase();

  function luhn(value) {
    const digits = String(value || "").replace(/[\s-]/g, "");
    if (!/^\d{13,19}$/.test(digits)) return false;
    let sum = 0;
    for (let i = 0; i < digits.length; i++) {
      let d = Number(digits[digits.length - 1 - i]);
      if (i % 2 === 1) {
        d *= 2;
        if (d > 9) d -= 9;
      }
      sum += d;
    }
    return sum % 10 === 0;
  }

  /** tokens splits "cardCvv", "card-cvv", "Card CVV" into ["card", "cvv"]. */
  function tokens(s) {
    return String(s || "")
      .replace(/([a-z])([A-Z])/g, "$1 $2")
      .toLowerCase()
      .split(/[^a-z0-9]+/)
      .filter(Boolean);
  }

  const SECRET_WORDS = new Set([
    "cvv", "cvv2", "cvc", "cvc2", "csc", "cvn", "ssn", "pin", "otp", "passcode", "totp", "2fa", "mfa",
    "pass", "password", "passwd", "pwd", "passphrase", "token", "secret", "apikey",
  ]);
  const everPassword = typeof WeakSet === "function" ? new WeakSet() : null;

  /** markPassword remembers a field that is (or was) type=password. */
  function markPassword(el) {
    if (everPassword && el && typeof el === "object") everPassword.add(el);
  }

  function cssMasked(el) {
    try {
      const cs = root.getComputedStyle && el && el.nodeType === 1 ? root.getComputedStyle(el) : null;
      const v = cs && (cs.webkitTextSecurity || cs.getPropertyValue("-webkit-text-security"));
      return !!v && v !== "none";
    } catch {
      return false;
    }
  }
  const SECRET_PAIRS = [
    ["security", "code"],
    ["social", "security"],
    ["one", "time"],
    ["verification", "code"],
    ["auth", "code"],
  ];

  /** looksSecret is true when a field's own description names a secret. */
  function looksSecret(text) {
    const t = tokens(text);
    if (t.some((w) => SECRET_WORDS.has(w))) return true;
    for (let i = 0; i + 1 < t.length; i++) {
      if (SECRET_PAIRS.some(([a, b]) => t[i] === a && t[i + 1] === b)) return true;
    }
    return false;
  }

  /**
   * maskReason says why a field's value must not be recorded, or "".
   * `label` is the field's label text when the caller knows it.
   */
  function maskReason(el, value, label) {
    return sensitiveField(el, label) || (luhn(value) ? "card" : "");
  }

  /** sensitiveField is maskReason without the value: the field itself is secret. */
  function sensitiveField(el, label) {
    const auto = String(attr(el, "autocomplete") || "").toLowerCase();
    const isInput = tagOf(el) === "input";
    if (isInput && typeOf(el) === "password") {
      markPassword(el);
      return "password";
    }
    if (everPassword && everPassword.has(el)) return "password";
    if (cssMasked(el)) return "password";
    if (/(^|\s)(current-password|new-password)$/.test(auto)) return "password";
    if (/(^|\s)cc-/.test(auto)) return "card";
    if (/(^|\s)one-time-code$/.test(auto)) return "otp";
    if (isInput && typeOf(el) === "hidden") return "hidden";
    const described = [attr(el, "name"), attr(el, "id"), attr(el, "placeholder"), attr(el, "aria-label"), label].join(" ");
    if (looksSecret(described)) return "secret";
    return "";
  }

  const slug = (s) =>
    String(s || "")
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "_")
      .replace(/^_+|_+$/g, "")
      .slice(0, 40);

  /** secretName suggests the secret input a masked value becomes. */
  function secretName(el, reason, label, looksGenerated) {
    const auto = String(attr(el, "autocomplete") || "").split(" ").pop();
    const id = attr(el, "id");
    const stableId = id && !(looksGenerated && looksGenerated(id)) ? id : "";
    const named = slug(attr(el, "name") || stableId || label || (auto !== "on" && auto !== "off" ? auto : ""));
    if (named) return named;
    return { password: "password", card: "card_number", otp: "one_time_code", hidden: "hidden_value" }[reason] || "secret";
  }

  /** scrubCards replaces every Luhn-valid card number in free text. */
  function scrubCards(text) {
    return String(text || "").replace(/\d(?:[ -]?\d){12,18}/g, (m) => (luhn(m) ? "[card]" : m));
  }

  // Query parameters whose values are credentials or one-time secrets.
  const SENSITIVE_PARAMS = new Set([
    "code", "token", "access_token", "id_token", "refresh_token", "state", "sig", "signature", "key", "api_key",
    "apikey", "password", "pass", "pwd", "auth", "session", "sessionid", "sid", "magic", "reset", "otp", "ticket",
  ]);

  const sensitiveParam = (name) => {
    const n = String(name).toLowerCase();
    return SENSITIVE_PARAMS.has(n) || /token|secret|signature|passw/.test(n);
  };

  /**
   * sanitizeUrl drops the fragment (unless it is a plain hash route such as
   * "#/inbox") and replaces sensitive query values with REDACTED. A
   * relative URL (a snippet's href) is judged against `base` and stays
   * relative; without a base, a value that is not a URL is unchanged.
   */
  function sanitizeUrl(raw, base) {
    const s = String(raw || "");
    let u;
    let relative = false;
    try {
      u = new URL(s);
    } catch {
      try {
        if (!base) return s;
        u = new URL(s, base);
        relative = true;
      } catch {
        return s;
      }
    }
    if (!/^(https?|file):$/.test(u.protocol)) return s;
    if (u.hash && !/^#\/[^=?&]*$/.test(u.hash)) u.hash = "";
    const keys = Array.from(u.searchParams.keys());
    for (const k of new Set(keys)) {
      if (sensitiveParam(k)) u.searchParams.set(k, "REDACTED");
    }
    return relative ? u.pathname + u.search + u.hash : u.toString();
  }

  // --- network calls (recorder_network.js) ---------------------------------
  //
  // A captured API call keeps header NAMES; a credential header (Authorization,
  // API keys, CSRF / session tokens, or any value that is a JWT or "Bearer x")
  // loses its value and is marked `credential` (Authorization keeps its scheme
  // word). Cookies are never seen by page code and are dropped by name anyway.
  // Bodies are JSON or forms with every secret-named key REDACTED, JWTs and card
  // numbers removed; anything else is dropped. internal/recording/netprivacy.go
  // applies the same rules again.

  const NET_BODY_MAX = 8192;
  const CREDENTIAL_HEADERS = new Set([
    "authorization", "proxy-authorization", "cookie", "x-api-key", "api-key", "apikey", "x-auth-token",
    "x-csrf-token", "x-xsrf-token", "csrf-token", "x-access-token",
  ]);
  const credentialHeader = (name) => {
    const n = String(name || "").trim().toLowerCase();
    return CREDENTIAL_HEADERS.has(n) || /token|secret|auth|api-?key|session|csrf|xsrf|signature|password|credential/.test(n);
  };
  const JWT = /eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}/;
  const credentialValue = (v) => JWT.test(String(v || "")) || /^(bearer|basic|token|digest)\s/i.test(String(v || "").trim());
  const BODY_SECRET =
    /pass|pwd|token|secret|signature|session|cookie|credential|private|auth($|[^o])|api_?key|(^|[^a-z])(pin|cvv|cvc|csc|otp|ssn|2fa|mfa|key|code)([^a-z]|$)/i;

  const redactString = (s) => (credentialValue(s) ? "REDACTED" : scrubCards(s));

  function redactJson(v, key) {
    if (key && BODY_SECRET.test(key) && (v === null || typeof v !== "object")) return "REDACTED";
    if (Array.isArray(v)) return v.map((x) => redactJson(x, key));
    if (v && typeof v === "object") {
      const out = {};
      for (const k of Object.keys(v)) out[k] = redactJson(v[k], k);
      return out;
    }
    return typeof v === "string" ? redactString(v) : v;
  }

  /** redactBody returns a JSON or form body with its secrets REDACTED, else "". */
  function redactBody(body, contentType) {
    const b = String(body || "").trim();
    if (!b) return "";
    let out = "";
    let parsed;
    let isJson = false;
    try {
      parsed = JSON.parse(b);
      isJson = true;
    } catch {
      // not JSON
    }
    if (isJson) {
      out = JSON.stringify(redactJson(parsed, ""));
    } else if (/x-www-form-urlencoded/i.test(contentType || "")) {
      out = b
        .split("&")
        .map((pair) => {
          const i = pair.indexOf("=");
          const k = i < 0 ? pair : pair.slice(0, i);
          const v = i < 0 ? "" : pair.slice(i + 1);
          let key = k;
          let val = v;
          try {
            key = decodeURIComponent(k.replace(/\+/g, " "));
            val = decodeURIComponent(v.replace(/\+/g, " "));
          } catch {
            // keep raw
          }
          if (BODY_SECRET.test(key) || sensitiveParam(key) || credentialValue(val)) return `${k}=REDACTED`;
          const r = redactString(val);
          return r === val ? pair : `${k}=${encodeURIComponent(r)}`;
        })
        .join("&");
    } else {
      return "";
    }
    return out.length > NET_BODY_MAX ? "" : out;
  }

  /**
   * redactNet turns what the page hook saw into a NetEntry
   * (internal/recording/types.go) that is safe to queue.
   */
  function redactNet(raw) {
    const r = raw || {};
    const headers = [];
    for (const h of Array.isArray(r.requestHeaders) ? r.requestHeaders : []) {
      const name = String((h && h.name) || "").trim();
      const lower = name.toLowerCase();
      if (!name || name.length > 100 || lower === "cookie" || lower === "set-cookie") continue;
      if (headers.length >= 40) break;
      const value = String((h && h.value) || "");
      if (credentialHeader(name) || credentialValue(value)) {
        const out = { name, credential: true };
        const scheme = value.trim().split(/\s+/)[0];
        if (/^(proxy-)?authorization$/.test(lower) && /^[A-Za-z][A-Za-z0-9-]{0,19}$/.test(scheme) && value.trim().indexOf(" ") > 0) {
          out.scheme = scheme;
        }
        headers.push(out);
      } else {
        headers.push({ name, value: value.slice(0, 200) });
      }
    }
    const entry = {
      t: Number(r.t) || 0,
      method: /^[A-Za-z]{1,16}$/.test(String(r.method || "")) ? String(r.method).toUpperCase() : "GET",
      url: sanitizeUrl(String(r.url || "")),
      status: Number(r.status) || 0,
    };
    if (r.contentType) entry.contentType = String(r.contentType).slice(0, 200);
    if (r.initiator === "fetch" || r.initiator === "xhr") entry.initiator = r.initiator;
    if (headers.length) entry.requestHeaders = headers;
    if (r.requestType) entry.requestType = String(r.requestType).slice(0, 200);
    const body = redactBody(r.requestBody, r.requestType);
    if (body) entry.requestBody = body;
    let sample = redactBody(r.responseSample, "application/json");
    try {
      JSON.parse(sample);
    } catch {
      sample = "";
    }
    if (sample) entry.responseSample = sample;
    return entry;
  }

  root.MonoRecorderPrivacy = {
    luhn, maskReason, sensitiveField, markPassword, secretName, scrubCards, looksSecret, tokens, slug, sanitizeUrl,
    credentialHeader, redactBody, redactNet,
  };
})(globalThis);
