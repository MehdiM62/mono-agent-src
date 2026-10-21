/**
 * MonoAgent Bridge — pairing without a step (the worker's half)
 *
 * A browser with no pairing token cannot learn that the person signed in to
 * MonoAgent: that arrives over a paired socket. So instead of asking the
 * person to run `extension pair`, the worker asks the bridge directly:
 *
 *   GET  /monoagent/status      {service, signedIn, paired, autoPair}
 *   POST /monoagent/pair/claim  {instance, label} -> {token, id}
 *
 * The bridge hands a token to a loopback request from an extension origin
 * while the account is signed in (internal/extension/autopair.go). `step`
 * decides what to do from what the bridge says; the worker acts on the answer:
 *
 *   claimed     the token to store and dial with;
 *   down        no bridge on any port: "MonoAgent isn't running";
 *   signed_out  the bridge is up and nobody is signed in: poll, claim the
 *               moment that changes;
 *   disabled    automatic pairing is off on this machine: the manual path;
 *   refused     the bridge turned the claim down (reason in `error`).
 *
 * Pure apart from the injected fetch, so it runs in node (autopair.test.mjs).
 */

(function (root) {
  "use strict";

  const SERVICE = "monoagent-bridge";
  const PORTS = [9222, 9323];
  const HOST = "127.0.0.1";
  const TIMEOUT_MS = 1500;
  // After a refused claim the worker does not ask again for this long: the
  // bridge rate-limits claims, and a refusal does not change by asking.
  const REFUSED_BACKOFF_MS = 60 * 1000;

  /** bases lists the http origins to ask, best first: the configured ones, then the defaults. */
  function bases(known) {
    const out = [];
    const add = (raw) => {
      const m = /^(?:wss?|https?):\/\/([^/?#]+)/i.exec(String(raw || "").trim());
      if (m && out.indexOf(`http://${m[1]}`) === -1) out.push(`http://${m[1]}`);
    };
    const k = known || {};
    add(k.wsUrl);
    add(k.pairedWsUrl);
    add(k.workingWsUrl);
    for (const p of PORTS) add(`ws://${HOST}:${p}/monoagent`);
    return out;
  }

  function withDeadline(fetchImpl, url, init, ms) {
    const controller = typeof AbortController === "function" ? new AbortController() : null;
    let timer;
    const deadline = new Promise((resolve) => {
      timer = setTimeout(() => {
        if (controller) controller.abort();
        resolve(null);
      }, ms);
    });
    const attempt = fetchImpl(url, Object.assign({ cache: "no-store", signal: controller ? controller.signal : undefined }, init)).catch(() => null);
    return Promise.race([attempt, deadline]).finally(() => clearTimeout(timer));
  }

  /** status asks one base; the document only counts when it says it is the bridge. */
  async function status(fetchImpl, base, ms) {
    const res = await withDeadline(fetchImpl, `${base}/monoagent/status`, { method: "GET" }, ms);
    if (!res || !res.ok) return null;
    try {
      const doc = await res.json();
      if (!doc || doc.service !== SERVICE) return null;
      return { signedIn: doc.signedIn === true, autoPair: doc.autoPair !== false, paired: doc.paired === true };
    } catch {
      return null;
    }
  }

  /**
   * step is one pass of the state machine. deps: fetch, known (stored wsUrl,
   * pairedWsUrl, workingWsUrl), instance, label, timeoutMs.
   */
  async function step(deps) {
    const d = deps || {};
    const fetchImpl = d.fetch || (typeof fetch === "function" ? fetch : null);
    if (!fetchImpl) return { kind: "down" };
    const ms = Number(d.timeoutMs) > 0 ? Number(d.timeoutMs) : TIMEOUT_MS;
    const list = bases(d.known);
    const found = await Promise.all(list.map((b) => status(fetchImpl, b, ms)));
    const at = found.findIndex((s) => s);
    if (at === -1) return { kind: "down" };
    const base = list[at];
    const s = found[at];
    const wsUrl = `ws://${base.slice("http://".length)}/monoagent`;
    if (!s.signedIn) return { kind: "signed_out", wsUrl };
    if (!s.autoPair) return { kind: "disabled", wsUrl };

    const res = await withDeadline(
      fetchImpl,
      `${base}/monoagent/pair/claim`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ instance: d.instance || "", label: d.label || "" }),
      },
      ms
    );
    if (!res) return { kind: "refused", error: "the bridge did not answer the pairing request", wsUrl };
    let body = null;
    try {
      body = await res.json();
    } catch {
      // not JSON: handled below
    }
    if (res.ok && body && typeof body.token === "string" && body.token) {
      return { kind: "claimed", token: body.token, id: String(body.id || ""), wsUrl };
    }
    return { kind: "refused", error: (body && body.error) || `the bridge refused to pair (${res.status})`, wsUrl };
  }

  root.MonoAutoPair = { step, bases, REFUSED_BACKOFF_MS, PORTS };
})(globalThis);
