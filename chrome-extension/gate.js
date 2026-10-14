/**
 * MonoAgent Bridge — the sign-in gate (the worker's half)
 *
 * Nothing in the extension works before the person signs in to monoes.me.
 * account_state.js decides what "signed in" means; this holds that decision
 * for the service worker and puts it in front of every way in:
 *
 *   - runtime messages (the side panel, the content scripts): refused with
 *     {ok:false, code:"login_required"} unless signed in, except the few the
 *     sign-in screen and pairing need (OPEN_MESSAGES);
 *   - runtime ports (the recorder's panel port): not handed on;
 *   - context menu clicks and keyboard commands: only open the side panel,
 *     which shows the sign-in screen.
 *
 * It does that by interposing on those four chrome events BEFORE any other
 * module registers a listener (background.js imports this first): every
 * listener added afterwards lands in a list this file dispatches, so a
 * feature added later is gated without anyone remembering to gate it.
 *
 * Signed out is also how the worker's socket behaves (background.js): only
 * the auth frame and `ping` go out, and the bridge's commands are refused.
 * Offline outboxes keep what they hold; they send it once signed in.
 *
 * Pure apart from the chrome objects it is handed, so it runs in node
 * (gate.test.mjs).
 */

(function (root) {
  "use strict";

  const LOGIN_MESSAGE = "Sign in to MonoAgent first: open the MonoAgent app, or run monoagentcli account login";
  const CODE_LOGIN_REQUIRED = "login_required";
  // What still answers signed out: the sign-in screen's status and "check
  // again", and pairing (the extension can only learn that the person signed
  // in over a paired socket, so pairing must stay possible).
  const OPEN_MESSAGES = ["get_status", "set_ws_url", "set_pairing_token", "account_recheck"];
  // A report that says what the stored one says is written again only this
  // often: the window is a day, a write per ping would be noise.
  const REWRITE_AFTER_MS = 10 * 60 * 1000;

  function refusal() {
    return { ok: false, error: LOGIN_MESSAGE, code: CODE_LOGIN_REQUIRED };
  }

  /**
   * create makes the worker's gate. deps: storage (chrome.storage.local),
   * now (a clock), openPanel(tab) (opens the side panel; called inside the
   * user's gesture, before anything is awaited).
   */
  function create(deps) {
    const A = root.MonoAccount;
    const storage = deps.storage;
    const now = deps.now || Date.now;
    const openPanel = deps.openPanel || (() => {});
    const listeners = [];
    let seen = null;
    let loaded = false;
    let last = false;

    const ready = Promise.resolve()
      .then(() => storage.get(A.SEEN_KEY))
      .then(
        (got) => {
          // A report that arrived while storage was being read is newer.
          if (!seen && got && got[A.SEEN_KEY]) seen = got[A.SEEN_KEY];
        },
        () => {}
      )
      .then(() => {
        loaded = true;
        check();
      });

    const signedIn = () => loaded && A.signedIn(seen, now());
    /** state is "in", "out", or "unknown" while the stored report is still being read. */
    const state = () => (!loaded ? "unknown" : signedIn() ? "in" : "out");

    /** check tells the listeners when the decision changed (a report, or the window running out). */
    function check() {
      const next = signedIn();
      if (next === last) return;
      last = next;
      for (const fn of listeners) {
        try {
          fn(next);
        } catch (err) {
          console.error("[monoagent] gate listener:", err && err.message);
        }
      }
    }

    /** observe reads one frame from the bridge; true when it was an account report. */
    function observe(frame) {
      const next = A.seenFrom(frame, now());
      if (!next) return false;
      const same = !!seen && seen.state === next.state && next.at >= seen.at && next.at - seen.at < REWRITE_AFTER_MS;
      if (!same) {
        seen = next;
        Promise.resolve(storage.set({ [A.SEEN_KEY]: next })).catch(() => {});
      }
      loaded = true;
      check();
      return true;
    }

    // ── The events every feature listens on ─────────────────────────────

    /** interpose takes over one event: listeners added from now on are dispatched by `make`. */
    function interpose(event, make) {
      if (!event || typeof event.addListener !== "function") return;
      const list = [];
      event.addListener(make(list));
      event.addListener = (fn) => {
        if (typeof fn === "function" && list.indexOf(fn) === -1) list.push(fn);
      };
      event.removeListener = (fn) => {
        const at = list.indexOf(fn);
        if (at !== -1) list.splice(at, 1);
      };
      event.hasListener = (fn) => list.indexOf(fn) !== -1;
      event.hasListeners = () => list.length > 0;
    }

    function each(list, args) {
      let async = false;
      for (const fn of list.slice()) {
        try {
          if (fn(...args) === true) async = true;
        } catch (err) {
          console.error("[monoagent]", err && err.message);
        }
      }
      return async;
    }

    /** messages answers as Chrome would, with the gate in front. */
    const messages = (list) => (msg, sender, respond) => {
      let answered = false;
      const once = (value) => {
        if (answered) return;
        answered = true;
        respond(value);
      };
      if (msg && OPEN_MESSAGES.indexOf(msg.type) !== -1) return each(list, [msg, sender, once]);
      const st = state();
      if (st === "in") return each(list, [msg, sender, once]);
      if (st === "out") {
        once(refusal());
        return false;
      }
      // The worker was woken by this message: decide once the report is read.
      ready.then(() => {
        if (!signedIn()) return once(refusal());
        if (!each(list, [msg, sender, once])) once(undefined);
      });
      return true;
    };

    /** ports are handed on only signed in. */
    const ports = (list) => (port) => {
      const st = state();
      if (st === "in") return void each(list, [port]);
      if (st === "unknown") ready.then(() => signedIn() && each(list, [port]));
    };

    /**
     * gestures (a menu click, a shortcut) open the side panel signed out.
     * Chrome opens a side panel only inside the gesture, so while the report
     * is still being read the panel is opened anyway and the feature runs
     * after, if signed in.
     */
    const gestures = (list) => (...args) => {
      const st = state();
      if (st === "in") return void each(list, args);
      const tab = args[1];
      try {
        openPanel(tab);
      } catch {
        // no side panel to open (no tab, no API): nothing else to do
      }
      if (st === "unknown") ready.then(() => signedIn() && each(list, args));
    };

    function install(chrome) {
      interpose(chrome.runtime && chrome.runtime.onMessage, messages);
      interpose(chrome.runtime && chrome.runtime.onConnect, ports);
      interpose(chrome.contextMenus && chrome.contextMenus.onClicked, gestures);
      interpose(chrome.commands && chrome.commands.onCommand, gestures);
    }

    return {
      ready,
      state,
      signedIn,
      seen: () => seen,
      observe,
      check,
      install,
      onChange: (fn) => listeners.push(fn),
    };
  }

  root.MonoGate = { create, refusal, LOGIN_MESSAGE, CODE_LOGIN_REQUIRED, OPEN_MESSAGES };
})(globalThis);
