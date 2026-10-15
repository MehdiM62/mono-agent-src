/**
 * MonoAgent Bridge — what the bridge says about the monoes.me account
 *
 * While MonoAgent has no valid sign-in the bridge answers every request but
 * `ping` with {kind: "reply", id, error, code: "account_locked"} (spec §6.3),
 * and `ping` says where the account stands in `data.account`. This reads those
 * frames into one fact the worker and the side panel share: is the bridge
 * refusing? Pure, so it runs in node (account_state.test.mjs).
 *
 * A failed reply has no "ok" key on the wire (internal/extension/request.go,
 * Reply.OK is omitempty), so a refusal is read from its code, never from
 * `ok === false`.
 */

(function (root) {
  "use strict";

  /** The code a refused request carries. */
  const CODE_ACCOUNT_LOCKED = "account_locked";
  const LOCKED = "locked";

  /**
   * fromReply reads one frame from the bridge and returns the account it
   * proves, or null when it proves nothing:
   *   - a failed reply with the account_locked code proves a locked, enforced
   *     account;
   *   - a successful answer to anything but ping proves the bridge is not
   *     refusing (a refusing bridge answers every other request with the code);
   *   - `ping` carries the state itself, but a locked state with no word on
   *     enforcement could be the warn period, when the bridge still answers
   *     everything: that proves nothing, so it is null and can neither clear a
   *     refusal that was proven nor invent one.
   */
  function fromReply(frame) {
    if (!frame || frame.kind !== "reply" || frame.progress) return null;
    if (!frame.ok && frame.code === CODE_ACCOUNT_LOCKED) {
      return { state: LOCKED, reason: "", enforced: true };
    }
    if (!frame.ok) return null;
    const data = frame.data || {};
    if (data.account === undefined) {
      return data.pong === true ? null : { state: "ok", reason: "", enforced: true };
    }
    const account = data.account;
    if (!account || typeof account !== "object" || typeof account.state !== "string") return null;
    if (account.state === LOCKED && typeof account.enforced !== "boolean") return null;
    return {
      state: account.state,
      reason: typeof account.reason === "string" ? account.reason : "",
      enforced: account.enforced !== false,
    };
  }

  /**
   * refusing is true when the bridge is turning requests away: locked, and
   * said to be enforced. An account that does not say is not a refusal.
   */
  function refusing(account) {
    return !!account && account.state === LOCKED && account.enforced === true;
  }

  /**
   * tracker holds the last account the bridge reported (null: unknown, which
   * counts as not refusing: a bridge that predates the gate never says). A
   * listener is called with (account, wasRefusing) whenever it changes.
   */
  function tracker() {
    let current = null;
    const listeners = [];
    return {
      get: () => current,
      refusing: () => refusing(current),
      set(next) {
        const before = current;
        const after = next || null;
        if (JSON.stringify(before) === JSON.stringify(after)) return;
        current = after;
        for (const fn of listeners) {
          try {
            fn(after, refusing(before));
          } catch {
            // A listener is decoration; it cannot stop the others.
          }
        }
      },
      onChange: (fn) => listeners.push(fn),
    };
  }

  // ── Signed in or not (the gate: gate.js, signin.html) ────────────────────

  /**
   * Nothing in the extension works until the person has signed in to
   * monoes.me. The extension cannot check a session itself: it only knows
   * what a bridge last told it. So "signed in" means exactly this:
   *
   *   the last account a bridge reported was ok or grace, and that report is
   *   less than CONFIRM_WINDOW_MS old.
   *
   * A report is a reply to `ping` (it says data.account) or any reply refused
   * with account_locked. A `ping` without an account is a bridge from before
   * the gate: that reports nobody signed in. Locked is signed out whether or
   * not the bridge says it is enforced. Nothing else counts: an ordinary
   * answer only shows the bridge did not refuse that one request.
   *
   * The window is the CLI's own grace (internal/account GraceWindow, 24h): a
   * session the CLI confirmed keeps working that long without monoes.me, so
   * an extension whose bridge is down keeps its offline behaviour that long
   * after the last time a bridge said "signed in". A fresh install has no
   * report and is signed out. A report stamped in the future (the clock went
   * back) proves nothing.
   */
  const SEEN_KEY = "accountSeen";
  const CONFIRM_WINDOW_MS = 24 * 60 * 60 * 1000;
  const CLOCK_SLACK_MS = 60 * 1000;
  const SIGNED_IN_STATES = ["ok", "grace"];

  /** seenFrom reads one frame into a report {state, at}, or null when it reports nothing. */
  function seenFrom(frame, now) {
    if (!frame || frame.kind !== "reply" || frame.progress) return null;
    if (!frame.ok && frame.code === CODE_ACCOUNT_LOCKED) return { state: LOCKED, at: now };
    if (!frame.ok || !frame.data || frame.data.pong !== true) return null;
    const account = frame.data.account;
    const state = account && typeof account === "object" && typeof account.state === "string" ? account.state : LOCKED;
    return { state, at: now };
  }

  /** signedIn is the gate's one decision, at time `now`. */
  function signedIn(seen, now) {
    if (!seen || typeof seen !== "object" || SIGNED_IN_STATES.indexOf(seen.state) === -1) return false;
    if (typeof seen.at !== "number" || !Number.isFinite(seen.at)) return false;
    return seen.at <= now + CLOCK_SLACK_MS && now - seen.at < CONFIRM_WINDOW_MS;
  }

  root.MonoAccount = {
    fromReply, refusing, tracker, seenFrom, signedIn,
    CODE_ACCOUNT_LOCKED, SEEN_KEY, CONFIRM_WINDOW_MS, CLOCK_SLACK_MS,
  };
})(globalThis);
