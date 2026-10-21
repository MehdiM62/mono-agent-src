/**
 * MonoAgent Bridge — the sign-in screen (signin.html)
 *
 * What the side panel shows while the worker's gate says nobody is signed in
 * (gate.js). It asks the worker for its status, says what to do, and goes
 * back to the full panel the moment the worker reports a sign-in. `view` is
 * pure, so the wording is tested in node (signin.test.mjs).
 */

(function (root) {
  "use strict";

  const LOGIN_COMMAND = "monoagentcli account login";
  const PAIR_COMMAND = "monoagentcli extension pair";
  const RECHECK_MS = 3000;

  function plural(n, one, many) {
    return `${n} ${n === 1 ? one : many}`;
  }

  /**
   * view turns the worker's status (get_status / account_recheck) into what
   * the screen says: {signedIn, pair, queued, state}. Empty strings are hidden.
   */
  function view(status) {
    const s = status || {};
    const out = { signedIn: s.signedIn === true, unpaired: false, pair: "", queued: "", state: "", code: "" };
    const unpaired = s.status === "unpaired";
    if (unpaired && s.reason === "bridge_signed_out") {
      // The bridge is up, so signing in is all it takes: this browser pairs
      // itself the moment the account is signed in, and this screen leaves.
      out.state = "Sign in in the MonoAgent app or run monoagentcli account login. This window updates automatically.";
    } else if (unpaired && s.reason === "pair_waiting") {
      // The bridge holds this browser's request until the owner decides.
      out.state = "Waiting for approval in the MonoAgent app (or run monoagentcli extension approve <id> --code <code>). This window updates automatically.";
      // The owner types this code to approve; a request they did not start shows it to nobody they can ask.
      if (s.code) out.code = `Code: ${s.code} \u2014 approve only the request that shows this code.`;
    } else if (unpaired && (s.reason === "pair_denied" || s.reason === "pair_expired")) {
      out.unpaired = true;
      out.state = s.reason === "pair_denied" ? "The request to pair this browser was denied." : "The request to pair this browser expired before it was approved.";
      out.pair = `Press Check again to ask once more, or run ${PAIR_COMMAND} and paste the token it prints below.`;
    } else if (unpaired) {
      // Automatic pairing is off or was refused: the manual path.
      out.unpaired = true;
      out.pair = `This browser could not connect to MonoAgent by itself. After you sign in, run ${PAIR_COMMAND}, or paste the token it prints below.`;
    }
    const q = s.queued || {};
    const parts = [];
    if (q.captures > 0) parts.push(plural(q.captures, "saved page", "saved pages"));
    if (q.tasks > 0) parts.push(plural(q.tasks, "task", "tasks"));
    if (q.recording) parts.push("a recording");
    if (parts.length) {
      out.queued = `Waiting in this browser: ${parts.join(", ")}. Nothing is sent until you sign in; then it is sent as usual.`;
    }
    if (unpaired) {
      // worded above
    } else if (s.status === "connected") out.state = "MonoAgent is running here, and says nobody is signed in.";
    else if (s.status === "connecting") out.state = "Looking for MonoAgent on this computer…";
    else if (s.status === "disconnected") out.state = "MonoAgent isn\u2019t running. Start the app or run monoagentcli extension serve. This window updates automatically.";
    return out;
  }

  root.MonoSignIn = { view, LOGIN_COMMAND, PAIR_COMMAND };

  if (typeof document === "undefined" || typeof chrome === "undefined" || !chrome.runtime) return;

  const $ = (id) => document.getElementById(id);

  // A status broadcast does not carry the outboxes; keep the last count.
  let queued = null;

  function show(status) {
    if (status && status.queued) queued = status.queued;
    const v = view(Object.assign({}, status, { queued }));
    if (v.signedIn) {
      location.replace("sidepanel.html");
      return;
    }
    $("signin-pairing").hidden = !v.unpaired;
    for (const [id, text] of [["signin-pair", v.pair], ["signin-queued", v.queued], ["signin-code", v.code], ["signin-state", v.state]]) {
      $(id).textContent = text;
      $(id).hidden = !text;
    }
  }

  function recheck(manual) {
    chrome.runtime.sendMessage({ type: "account_recheck", manual }, (reply) => {
      void chrome.runtime.lastError; // a worker still waking answers the next time
      if (reply) show(reply);
    });
  }

  $("signin-pair-save").addEventListener("click", () => {
    const input = $("signin-pair-token");
    const value = input.value.trim();
    if (!value) {
      input.focus();
      return;
    }
    chrome.runtime.sendMessage({ type: "set_pairing_token", value }, (reply) => {
      void chrome.runtime.lastError;
      if (reply && reply.ok) {
        input.value = "";
        $("signin-state").textContent = "Saved. Connecting…";
        $("signin-state").hidden = false;
      } else {
        $("signin-state").textContent = (reply && reply.error) || "Could not save the pairing token.";
        $("signin-state").hidden = false;
      }
    });
  });

  chrome.runtime.onMessage.addListener((msg) => {
    if (msg && msg.type === "status") show(msg);
  });
  $("signin-recheck").addEventListener("click", () => recheck(true));
  $("signin-copy").addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(LOGIN_COMMAND);
      $("signin-copy").textContent = "Copied";
    } catch {
      // no clipboard: the command is on screen to select
    }
  });
  recheck();
  // The bridge is asked again every few seconds while this screen is on: a
  // sign-in in the app, or a bridge started after the browser, flips it by
  // itself (the worker pairs this browser the moment the account is signed in).
  setInterval(() => {
    if (document.visibilityState !== "hidden") recheck();
  }, RECHECK_MS);
})(globalThis);
