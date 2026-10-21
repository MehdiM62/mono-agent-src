// The sign-in screen's words (signin.js `view`): what the person reads while
// nothing works, and that it leaves the moment a bridge reports a sign-in.
//
// `node --test chrome-extension/signin.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const { MonoSignIn: S } = loadExtensionScripts(["signin.js"]);

test("signed in leaves the screen; anything else stays on it", () => {
  assert.equal(S.view({ signedIn: true, status: "connected" }).signedIn, true);
  assert.equal(S.view({ signedIn: false, status: "connected" }).signedIn, false);
  assert.equal(S.view({ signedIn: "yes" }).signedIn, false);
  assert.equal(S.view(undefined).signedIn, false);
});

test("it says where MonoAgent stands", () => {
  assert.match(S.view({ status: "connected" }).state, /nobody is signed in/);
  assert.match(S.view({ status: "disconnected" }).state, /isn\u2019t running.*monoagentcli extension serve/);
  assert.match(S.view({ status: "connecting" }).state, /Looking for MonoAgent/);
});

test("a bridge nobody is signed in to: sign in, and the window updates by itself", () => {
  const v = S.view({ status: "unpaired", reason: "bridge_signed_out" });
  assert.match(v.state, /Sign in in the MonoAgent app or run monoagentcli account login\. This window updates automatically/);
  assert.equal(v.pair, "");
  assert.equal(v.unpaired, false, "no token field: this browser pairs itself");
});

test("a request waiting for the owner says where to approve it, and offers no token box", () => {
  const v = S.view({ status: "unpaired", reason: "pair_waiting" });
  assert.match(v.state, /Waiting for approval in the MonoAgent app \(or run monoagentcli extension approve --latest\)/);
  assert.equal(v.unpaired, false);
  assert.equal(v.pair, "");
});

test("denied and expired requests say so and offer asking again or the manual path", () => {
  const d = S.view({ status: "unpaired", reason: "pair_denied" });
  assert.match(d.state, /denied/);
  const e = S.view({ status: "unpaired", reason: "pair_expired" });
  assert.match(e.state, /expired/);
  for (const v of [d, e]) {
    assert.equal(v.unpaired, true);
    assert.match(v.pair, /Check again.*monoagentcli extension pair/);
  }
});

test("automatic pairing off or refused: the manual path, kept as the fallback", () => {
  for (const reason of ["autopair_off", "auth_rejected", ""]) {
    const v = S.view({ status: "unpaired", reason });
    assert.match(v.pair, /run monoagentcli extension pair, or paste the token/);
    assert.equal(v.unpaired, true);
  }
  assert.equal(S.view({ status: "connected" }).pair, "");
  assert.equal(S.view({ status: "connected" }).unpaired, false);
});

test("what is kept is named, and said not to be sent until sign-in", () => {
  const v = S.view({ status: "disconnected", queued: { captures: 2, tasks: 1, recording: true } });
  assert.equal(v.queued, "Waiting in this browser: 2 saved pages, 1 task, a recording. Nothing is sent until you sign in; then it is sent as usual.");
  assert.equal(S.view({ queued: { captures: 0, tasks: 0, recording: false } }).queued, "");
});
