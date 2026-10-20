// Tests for the profile a capture is saved into: the sticky choice, the
// per-save override, and every way the answer can be "just save it".
// `node --test 'chrome-extension/*.test.mjs'`.

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const { MonoCaptureProfile: P } = loadExtensionScripts(["capture_profile.js"]);

function fakeStorage(seed = {}) {
  const data = Object.assign({}, seed);
  return {
    data,
    get: async (keys) => {
      const wanted = Array.isArray(keys) ? keys : [keys];
      const out = {};
      for (const k of wanted) if (k in data) out[k] = data[k];
      return out;
    },
    set: async (values) => Object.assign(data, values),
  };
}

/** brokenStorage is a private window: every call throws. */
const brokenStorage = {
  get: async () => {
    throw new Error("no storage");
  },
  set: async () => {
    throw new Error("no storage");
  },
};

const PROFILES = [
  { id: "p-home", name: "Personal", default: false },
  { id: "p-work", name: "Work", default: true },
];

const settle = () => new Promise((r) => setTimeout(r, 5));

/** fakeAsk stands in for MonoAsk. */
function fakeAsk(reply, opts = {}) {
  const calls = [];
  return {
    calls,
    supports: async () => opts.supports !== false,
    request: async (method, params, options) => {
      calls.push({ method, params, options });
      if (opts.fail) throw Object.assign(new Error("offline"), { code: "offline" });
      return reply;
    },
  };
}

// --- the id is a directory name on the other side -------------------------

test("a profile id that could escape the profiles root is refused", () => {
  for (const id of ["../evil", "../../etc/passwd", "a/b", "a\\b", "..", "a..b", "", "   ", null, undefined, 7, {}]) {
    assert.equal(P.isValidProfileId(id), false, `accepted ${JSON.stringify(id)}`);
  }
  for (const id of ["default", "p-work", "org_1", "a.b", "6f1c2f18-0e5b-4a4c-8f3a-8a2d9a1b0c11"]) {
    assert.equal(P.isValidProfileId(id), true, `rejected ${id}`);
  }
});

test("a hostile id never reaches the envelope", () => {
  assert.deepEqual(P.applyToMeta({ url: "https://e.com" }, "../evil"), { url: "https://e.com" });
  assert.deepEqual(P.applyToMeta({ url: "https://e.com" }, "p-work"), {
    url: "https://e.com",
    profile: "p-work",
  });
  // An earlier stamp is removed rather than left behind when the choice
  // becomes "no profile".
  assert.deepEqual(P.applyToMeta({ profile: "p-work" }, ""), {});
});

test("a list from the backend is filtered, not trusted", () => {
  const got = P.normalizeProfiles([
    { id: "../evil", name: "Evil" },
    { id: "p-work", name: "  Work  ", default: true },
    { id: "p-work", name: "Duplicate" },
    { id: "p-home" },
    "nonsense",
    null,
  ]);
  assert.deepEqual(got, [
    { id: "p-work", name: "Work", default: true },
    { id: "p-home", name: "p-home", default: false },
  ]);
});

// --- choosing -------------------------------------------------------------

test("with nothing stored, the default profile is chosen", () => {
  const got = P.choose("", PROFILES);
  assert.equal(got.id, "p-work");
  assert.equal(got.changed, false);
});

test("a stored profile that still exists is kept, silently", () => {
  const got = P.choose("p-home", PROFILES);
  assert.deepEqual([got.id, got.name, got.changed], ["p-home", "Personal", false]);
});

test("a stored profile that is gone falls back to the default, and says so", () => {
  const got = P.choose("p-deleted", PROFILES);
  assert.equal(got.id, "p-work");
  assert.equal(got.changed, true);
  assert.match(got.reason, /gone/);
  assert.match(got.reason, /Work/);
});

test("no profiles configured means no profile on the capture", () => {
  const got = P.choose("", []);
  assert.deepEqual([got.id, got.changed], ["", false]);
});

test("with the bridge down the remembered choice is used as-is", () => {
  const got = P.choose("p-home", [], { offline: true });
  assert.deepEqual([got.id, got.changed], ["p-home", false]);
  // …and with nothing remembered, the capture is simply unprofiled.
  assert.equal(P.choose("", [], { offline: true }).id, "");
});

test("a stored id that is not usable is treated as nothing stored", () => {
  assert.equal(P.choose("../evil", PROFILES).id, "p-work");
  assert.equal(P.choose("../evil", PROFILES).changed, false);
  assert.equal(P.choose("../evil", [], { offline: true }).id, "");
});

// --- storage --------------------------------------------------------------

test("the choice is remembered, and a hostile one is not", async () => {
  const storage = fakeStorage();
  assert.equal(await P.remember(storage, "p-work"), "p-work");
  assert.equal(storage.data[P.PROFILE_KEY], "p-work");
  assert.equal(await P.remember(storage, "../evil"), "");
  assert.equal(storage.data[P.PROFILE_KEY], "");
});

test("a profile-less browser still captures", async () => {
  assert.deepEqual(await P.load(brokenStorage), { profile: "", profiles: [], asked: false });
  assert.equal(await P.remember(brokenStorage, "p-work"), "p-work");
  assert.equal(await P.sticky(brokenStorage), "");
});

// --- asking the backend ---------------------------------------------------

test("profile.list fills the picker", async () => {
  const ask = fakeAsk({ profiles: PROFILES, default: "p-work" });
  const got = await P.fetchProfiles(ask);
  assert.equal(got.offline, false);
  assert.deepEqual(got.profiles.map((p) => p.id), ["p-home", "p-work"]);
  assert.equal(ask.calls[0].method, "profile.list");
  assert.equal(ask.calls[0].options.timeoutMs, P.LIST_TIMEOUT_MS);
});

test("a bridge that is down, old or silent is 'offline', never a failure", async () => {
  assert.deepEqual(await P.fetchProfiles(fakeAsk(null, { fail: true })), { profiles: [], offline: true });
  assert.deepEqual(await P.fetchProfiles(fakeAsk(null, { supports: false })), { profiles: [], offline: true });
  assert.deepEqual(await P.fetchProfiles(null), { profiles: [], offline: true });
  assert.deepEqual(await P.fetchProfiles({}), { profiles: [], offline: true });
});

test("refresh caches the list and settles the choice", async () => {
  const storage = fakeStorage();
  const got = await P.refresh(fakeAsk({ profiles: PROFILES }), storage);
  assert.equal(got.id, "p-work");
  assert.equal(got.offline, false);
  assert.deepEqual(storage.data[P.PROFILES_KEY].map((p) => p.id), ["p-home", "p-work"]);
  // The fallback is remembered, so the next shortcut agrees with the popup.
  assert.equal(storage.data[P.PROFILE_KEY], "p-work");
});

test("refresh over a dead bridge draws from the cache and keeps the choice", async () => {
  const storage = fakeStorage({
    [P.PROFILE_KEY]: "p-home",
    [P.PROFILES_KEY]: PROFILES,
  });
  const got = await P.refresh(fakeAsk(null, { fail: true }), storage);
  assert.equal(got.offline, true);
  assert.equal(got.id, "p-home");
  assert.deepEqual(got.profiles.map((p) => p.id), ["p-home", "p-work"]);
});

test("refresh reports a stored profile that has since been deleted", async () => {
  const storage = fakeStorage({ [P.PROFILE_KEY]: "p-deleted" });
  const got = await P.refresh(fakeAsk({ profiles: PROFILES }), storage);
  assert.equal(got.id, "p-work");
  assert.equal(got.changed, true);
  // And it is remembered, so the shortcut does not keep using the ghost.
  assert.equal(storage.data[P.PROFILE_KEY], "p-work");
});

// --- the shortcut ---------------------------------------------------------

test("the shortcut path reads storage alone — zero interaction, no bridge", async () => {
  const storage = fakeStorage({ [P.PROFILE_KEY]: "p-home", [P.PROFILES_KEY]: PROFILES });
  assert.equal(await P.sticky(storage), "p-home");

  // A remembered profile that no longer exists still resolves to something
  // saveable rather than blocking the capture.
  const stale = fakeStorage({ [P.PROFILE_KEY]: "p-deleted", [P.PROFILES_KEY]: PROFILES });
  assert.equal(await P.sticky(stale), "p-work");

  // Nothing remembered and nothing cached: an unprofiled capture, exactly
  // as before this feature existed.
  assert.equal(await P.sticky(fakeStorage()), "");
});

test("the first shortcut capture on a fresh browser asks once, then never again", async () => {
  const storage = fakeStorage();
  const ask = fakeAsk({ profiles: PROFILES });

  // Nothing stored and nobody has ever asked: this one capture finds out.
  assert.equal(await P.stickyOrAsk(ask, storage, true), "p-work");
  assert.equal(ask.calls.length, 1);

  // From here it is storage alone, whatever else happens.
  assert.equal(await P.stickyOrAsk(ask, storage, true), "p-work");
  assert.equal(ask.calls.length, 1);
});

test("an install with no profiles asks once and then stops asking", async () => {
  const storage = fakeStorage();
  const ask = fakeAsk({ profiles: [] });

  assert.equal(await P.stickyOrAsk(ask, storage, true), "");
  assert.equal(await P.stickyOrAsk(ask, storage, true), "");
  assert.equal(ask.calls.length, 1, "an empty list is an answer, not a reason to keep asking");
});

test("a disconnected bridge is never asked, and never blocks the capture", async () => {
  const ask = fakeAsk(null, { fail: true });
  assert.equal(await P.stickyOrAsk(ask, fakeStorage(), false), "");
  assert.equal(ask.calls.length, 0);
  // Connected but broken is still just "no profile".
  assert.equal(await P.stickyOrAsk(ask, fakeStorage(), true), "");
});

test("a remembered choice that still exists is kept: checking the list does not change it", async () => {
  const ask = fakeAsk({ profiles: PROFILES });
  const storage = fakeStorage({ [P.PROFILE_KEY]: "p-home" });
  assert.equal(await P.stickyOrAsk(ask, storage, true), "p-home");
  await settle();
  assert.equal(await P.stickyOrAsk(ask, storage, true), "p-home");
  assert.equal(storage.data[P.PROFILE_KEY], "p-home");
});

// #822: the selected profile deleted in MonoAgent. The cached list still had
// it, so every lookup went to a profile that no longer exists until someone
// opened the side panel. A lookup made while connected now checks the list
// in the background (never making the caller wait), at most once a minute.
test("a profile deleted in MonoAgent drops out of the cache, and lookups fall back, without the side panel", async () => {
  const ask = fakeAsk({ profiles: [{ id: "p-work", name: "Work", default: true }] });
  const storage = fakeStorage({ [P.PROFILE_KEY]: "p-home", [P.PROFILES_KEY]: PROFILES });
  assert.equal(await P.stickyOrAsk(ask, storage, true), "p-home", "the caller is answered from the cache, at once");
  await settle();
  assert.equal(ask.calls.length, 1);
  assert.deepEqual(storage.data[P.PROFILES_KEY].map((p) => p.id), ["p-work"], "the cache knows it is gone");
  assert.equal(await P.stickyOrAsk(ask, storage, true), "p-work", "the next lookup falls back");
  assert.equal(await P.sticky(storage), "p-work", "so does the shortcut");
  assert.equal(storage.data[P.PROFILE_KEY], "p-home", "the choice is kept, so the side panel can say it was deleted");
  await settle();
  assert.equal(ask.calls.length, 1, "checked at most once a minute");
});

test("the background check never asks a disconnected bridge, and a dead one changes nothing", async () => {
  const quiet = fakeAsk({ profiles: [] });
  const storage = fakeStorage({ [P.PROFILE_KEY]: "p-home", [P.PROFILES_KEY]: PROFILES });
  assert.equal(await P.stickyOrAsk(quiet, storage, false), "p-home");
  await settle();
  assert.equal(quiet.calls.length, 0);
  const dead = fakeAsk(null, { fail: true });
  const other = fakeStorage({ [P.PROFILE_KEY]: "p-home", [P.PROFILES_KEY]: PROFILES });
  assert.equal(await P.stickyOrAsk(dead, other, true), "p-home");
  await settle();
  assert.deepEqual(other.data[P.PROFILES_KEY], PROFILES, "an unanswered check keeps the cache");
});

// D2 (#822): a capture is filed when it is sent, into the remembered profile
// or else the default, and a sender asks the bridge to find out.
test("profileForSending is the remembered profile, else the default, asking when it must", async () => {
  assert.equal(await P.profileForSending(fakeAsk({ profiles: PROFILES }), fakeStorage({ [P.PROFILE_KEY]: "p-home" })), "p-home");
  assert.equal(await P.profileForSending(fakeAsk({ profiles: PROFILES }), fakeStorage()), "p-work");
  assert.equal(await P.profileForSending(fakeAsk({ profiles: [] }), fakeStorage()), "", "no profile at all");
  // An empty list was the answer once; a profile made since is found.
  const storage = fakeStorage({ [P.PROFILES_KEY]: [] });
  assert.equal(await P.profileForSending(fakeAsk({ profiles: PROFILES }), storage), "p-work");
});

test("stampEnvelope files an unfiled envelope, leaves a filed one alone, and has nothing to say without a profile", async () => {
  const unfiled = { id: "a", meta: { title: "T" } };
  const stamped = await P.stampEnvelope(unfiled, async () => "p-work");
  assert.equal(stamped.meta.profile, "p-work");
  assert.equal(stamped.meta.title, "T");
  assert.equal(unfiled.meta.profile, undefined, "the queued envelope is not changed in place");
  let asked = 0;
  const filed = { id: "b", meta: { profile: "p-home" } };
  assert.equal(await P.stampEnvelope(filed, async () => (asked++, "p-work")), filed);
  assert.equal(asked, 0, "nobody is asked for a capture that already names its profile");
  assert.equal(await P.stampEnvelope(unfiled, async () => ""), null);
  assert.equal(await P.stampEnvelope(unfiled, async () => "../evil"), null);
});

// #862: a queued capture can name a profile deleted since it was queued. A
// flush or a retry (preparer with verify) checks it against the live list.
test("a verifying preparer files a capture whose profile is gone into the default, or holds it", async () => {
  const ask = fakeAsk({ profiles: PROFILES });
  const prepare = P.preparer(ask, fakeStorage(), { verify: true });
  const gone = { id: "g", meta: { title: "T", profile: "p-gone" }, warnings: ["w0"] };
  const filed = await prepare(gone);
  assert.equal(filed.meta.profile, "p-work");
  assert.equal(filed.meta.title, "T");
  assert.deepEqual(filed.warnings, ["w0", 'the profile "p-gone" no longer exists: filed into Work instead']);
  assert.equal(gone.meta.profile, "p-gone", "the queued envelope is not changed in place");
  const live = { id: "h", meta: { profile: "p-home" } };
  assert.equal(await prepare(live), live, "a live profile is left alone");
  assert.equal(ask.calls.length, 1, "the list is asked for once per run");

  const none = P.preparer(fakeAsk({ profiles: [] }), fakeStorage(), { verify: true });
  assert.equal(await none(gone), null, "no profile to file it into: held");
});

test("a named profile is kept when nobody can list profiles, and not checked without verify", async () => {
  const gone = { id: "g", meta: { profile: "p-gone" } };
  assert.equal(await P.preparer(fakeAsk(null, { fail: true }), fakeStorage(), { verify: true })(gone), gone);
  const ask = fakeAsk({ profiles: PROFILES });
  assert.equal(await P.preparer(ask, fakeStorage())(gone), gone, "a fresh capture is not checked here");
  assert.equal(ask.calls.length, 0);
});
