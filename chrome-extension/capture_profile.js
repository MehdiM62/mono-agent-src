/**
 * MonoAgent Bridge — which profile a capture is saved into
 *
 * A page saved from work should not turn up in a search of the personal
 * brain, so a capture can name the profile it belongs to: `meta.profile`
 * decides the inbox it lands in and, downstream, the store that ingests it.
 *
 * Two rules shape this file, both of them the user's:
 *
 *   STICKY, WITH A PER-SAVE OVERRIDE. The choice is remembered in
 *   chrome.storage, so the keyboard shortcut saves into the right profile
 *   with zero interaction; the popup's picker changes it for this save and
 *   the next, which is what "sticky" means here.
 *
 *   NEVER BLOCK A CAPTURE. The profile list comes from the Go bridge
 *   (profile.list), and the bridge is frequently not there. Every failure
 *   below resolves to "take it without a profile for now" — a page is never
 *   lost, and the capture never waits on a question about filing. It is
 *   filed when it is sent (profileForSending): there is no Unassigned pile
 *   (decision D2), so with no profile at all it stays queued until one
 *   exists.
 *
 * The id is used as a directory name on the other side. The Go side owns
 * that guarantee (internal/profiledir rejects separators and '..'), but a
 * hostile id must not be sent at all: isValidProfileId is that check, kept
 * deliberately identical to profiledir.ValidProfileID.
 */

(function (root) {
  "use strict";

  // The remembered choice, and the last list the bridge gave us. The list
  // is cached so the popup can draw a picker while the bridge is down —
  // it is display data, and the id is re-checked against a live list
  // whenever there is one.
  const PROFILE_KEY = "captureProfile";
  const PROFILES_KEY = "captureProfilesCache";

  // profile.list is a question about local state, so it answers fast or it
  // is not going to. The popup would rather draw without a picker than
  // hold the user up.
  const LIST_TIMEOUT_MS = 4000;

  const METHOD = "profile.list";

  // How long a lookup trusts the cached list before checking it again in the
  // background (#822): a profile deleted in MonoAgent must not stay the one
  // every lookup asks about until someone opens the side panel. Per storage,
  // and in memory, so a fresh worker checks on its first lookup.
  const RECHECK_MS = 60000;
  const checkedAt = new WeakMap();

  /**
   * isValidProfileId mirrors profiledir.ValidProfileID (Go): non-empty, no
   * path separators, no parent-directory component. Anything else is not
   * sent — a capture without a profile is fine; a capture that talks a
   * backend into writing outside the profiles root is not.
   */
  function isValidProfileId(id) {
    const value = typeof id === "string" ? id.trim() : "";
    if (!value) return false;
    if (value.includes("/") || value.includes("\\")) return false;
    if (value.includes("..")) return false;
    return true;
  }

  /** normalizeProfiles keeps the rows a picker can actually offer. */
  function normalizeProfiles(list) {
    const out = [];
    const seen = new Set();
    for (const raw of Array.isArray(list) ? list : []) {
      const id = raw && typeof raw.id === "string" ? raw.id.trim() : "";
      if (!isValidProfileId(id) || seen.has(id)) continue;
      seen.add(id);
      out.push({
        id,
        name: (raw.name && String(raw.name).trim()) || id,
        default: raw.default === true,
      });
    }
    return out;
  }

  /** defaultProfile is the row the backend marked, else the first one. */
  function defaultProfile(profiles) {
    const list = Array.isArray(profiles) ? profiles : [];
    return list.find((p) => p.default) || list[0] || null;
  }

  /**
   * choose settles which profile this capture is saved into, and says so in
   * a form the popup can show the user. The cases, in order:
   *
   *   - nothing stored: the default profile (or no profile at all, when
   *     none are configured);
   *   - stored and still there: that one, silently;
   *   - stored but GONE from a live list: the default, and `changed` is set
   *     so the popup can say the old profile is no longer there. Silently
   *     saving into a different brain than the one last chosen is the one
   *     outcome worth interrupting for;
   *   - no list at all (bridge down): the stored id is kept and used as-is.
   *     It was valid when it was chosen, and the alternative is refusing to
   *     save while offline.
   */
  function choose(stored, profiles, opts) {
    const known = normalizeProfiles(profiles);
    const wanted = isValidProfileId(stored) ? stored.trim() : "";
    const offline = !!(opts && opts.offline);

    if (!known.length) {
      // No list: either there are genuinely no profiles, or nobody could
      // be asked. Offline keeps the remembered choice; a live, empty list
      // means this install has no profiles and captures are unprofiled.
      if (offline && wanted) return { id: wanted, name: wanted, changed: false, reason: "" };
      return { id: "", name: "", changed: false, reason: "" };
    }

    const match = known.find((p) => p.id === wanted);
    if (match) return { id: match.id, name: match.name, changed: false, reason: "" };

    const fallback = defaultProfile(known);
    if (!wanted) {
      return { id: fallback.id, name: fallback.name, changed: false, reason: "" };
    }
    return {
      id: fallback.id,
      name: fallback.name,
      changed: true,
      reason: `the profile you last saved into is gone — using ${fallback.name}`,
    };
  }

  /** load reads the remembered choice and the cached list. */
  async function load(storage) {
    let stored = {};
    try {
      stored = (await storage.get([PROFILE_KEY, PROFILES_KEY])) || {};
    } catch {
      // No storage (private window, wiped profile): captures still save,
      // they just cannot remember where.
    }
    return {
      profile: typeof stored[PROFILE_KEY] === "string" ? stored[PROFILE_KEY] : "",
      profiles: normalizeProfiles(stored[PROFILES_KEY]),
      // Whether the backend has ever been asked. An empty list and a list
      // nobody has fetched look identical otherwise, and they mean
      // opposite things: "this install has no profiles" versus "we have
      // not found out yet".
      asked: Object.prototype.hasOwnProperty.call(stored, PROFILES_KEY),
    };
  }

  /** remember stores the choice for the next capture, shortcut included. */
  async function remember(storage, id) {
    const value = isValidProfileId(id) ? id.trim() : "";
    try {
      await storage.set({ [PROFILE_KEY]: value });
    } catch {
      // Stickiness is a convenience; failing to store it must not fail a
      // capture.
    }
    return value;
  }

  /** cacheProfiles keeps the last known list for an offline popup. */
  async function cacheProfiles(storage, profiles) {
    const list = normalizeProfiles(profiles);
    try {
      await storage.set({ [PROFILES_KEY]: list });
    } catch {
      // See remember().
    }
    return list;
  }

  /**
   * fetchProfiles asks the backend for the real list. `ask` is MonoAsk (or
   * anything with the same request/supports pair). Never throws: a backend
   * that is down, old, or slow yields `{ profiles: [], offline: true }`,
   * which choose() reads as "keep what you had".
   */
  async function fetchProfiles(ask) {
    if (!ask || typeof ask.request !== "function") return { profiles: [], offline: true };
    try {
      if (typeof ask.supports === "function" && !(await ask.supports(METHOD))) {
        // An older backend with no profile.list at all. Not an error: it
        // has no profiles to offer, and saying so beats a 4s timeout.
        return { profiles: [], offline: true };
      }
      const data = await ask.request(METHOD, {}, {
        timeoutMs: LIST_TIMEOUT_MS,
        idleTimeoutMs: LIST_TIMEOUT_MS,
      });
      return { profiles: normalizeProfiles(data && data.profiles), offline: false };
    } catch {
      return { profiles: [], offline: true };
    }
  }

  /**
   * refresh is fetch + cache + choose in one call: what the popup runs when
   * it opens, and what the worker runs before a shortcut capture when it
   * has a connection to spare. Returns the choice and the list to draw.
   */
  async function refresh(ask, storage) {
    const [{ profile, profiles: cached }, live] = await Promise.all([load(storage), fetchProfiles(ask)]);
    const profiles = live.offline ? cached : live.profiles;
    if (!live.offline) {
      checkedAt.set(storage, Date.now());
      await cacheProfiles(storage, live.profiles);
    }
    const chosen = choose(profile, profiles, { offline: live.offline });
    // A fallback is remembered, so the next shortcut capture agrees with
    // what the popup just showed.
    if (chosen.id !== profile) await remember(storage, chosen.id);
    return { profiles, offline: live.offline, ...chosen };
  }

  /**
   * sticky is the zero-interaction path: what the keyboard shortcut saves
   * into. Storage only — it never asks the bridge, because the answer has
   * to be ready before the capture starts and a shortcut capture must not
   * wait on the network.
   */
  async function sticky(storage) {
    const { profile, profiles } = await load(storage);
    return choose(profile, profiles, { offline: true }).id;
  }

  /**
   * stickyOrAsk is sticky for a caller that CAN ask, and is the shortcut
   * path's actual entry point. Its answer comes from storage in every
   * ordinary case (connected, the list is checked again in the background:
   * recheck); the one exception is a browser that has never asked what
   * profiles exist — where a shortcut capture would otherwise be filed
   * nowhere until the popup happened to be opened once. That first capture
   * asks, so "the default profile" is the default from the beginning.
   *
   * Still zero interaction, and still incapable of blocking a capture: the
   * question only goes out when the bridge is already connected, it is
   * bounded by LIST_TIMEOUT_MS, and every failure resolves to "no profile".
   */
  async function stickyOrAsk(ask, storage, connected) {
    const state = await load(storage);
    const chosen = choose(state.profile, state.profiles, { offline: true });
    if (chosen.id || state.asked || !connected) {
      if (connected) recheck(ask, storage);
      return chosen.id;
    }
    return (await refresh(ask, storage)).id;
  }

  /**
   * recheck asks for the list again, in the background and at most every
   * RECHECK_MS, and caches a live answer: the next sticky lookup then falls
   * back from a profile that is gone (choose). The remembered choice is left
   * as it is, so the side panel can still say the profile was deleted. Never
   * awaited by a capture; a bridge that does not answer changes nothing.
   */
  function recheck(ask, storage) {
    if (Date.now() - (checkedAt.get(storage) || 0) < RECHECK_MS) return;
    checkedAt.set(storage, Date.now());
    fetchProfiles(ask)
      .then((live) => (live.offline ? null : cacheProfiles(storage, live.profiles)))
      .catch(() => {});
  }

  /**
   * profileForSending is the profile a capture that names none is filed
   * into when it is sent: the remembered one, else the default, asking the
   * bridge (only a sender calls this, so the bridge is there). "" when no
   * profile exists at all. There is no Unassigned pile (decision D2), so the
   * caller then keeps the capture queued, unsent, until one does.
   */
  async function profileForSending(ask, storage) {
    const id = await stickyOrAsk(ask, storage, true);
    if (id) return id;
    // An empty list may be an old answer: a profile made since is asked for.
    return (await refresh(ask, storage)).id;
  }

  /**
   * stampEnvelope files an envelope that names no profile into the one
   * resolveId() answers, asked only then. A filed envelope is returned as it
   * is; null means there is no profile to file it into. The envelope passed
   * in is not changed: the queue keeps it as it was.
   */
  async function stampEnvelope(envelope, resolveId) {
    const meta = (envelope && envelope.meta) || {};
    if (isValidProfileId(meta.profile)) return envelope;
    const id = await resolveId();
    if (!isValidProfileId(id)) return null;
    return Object.assign({}, envelope, { meta: applyToMeta(Object.assign({}, meta), id) });
  }

  /**
   * preparer is the prepare(envelope) for one run of sends (a flush, one
   * capture, a retry): stampEnvelope with the profile asked for once, and
   * only if an envelope names none. null from it means: keep it queued.
   *
   * opts.verify (a flush, a retry) also checks a profile an envelope names
   * against the live list, asked for once (refileGone): a capture queued
   * before its profile was deleted still names it.
   */
  function preparer(ask, storage, opts) {
    let asked = null;
    let listed = null;
    const resolveId = () => (asked = asked || profileForSending(ask, storage));
    const listLive = () => (listed = listed || fetchProfiles(ask));
    const verify = !!(opts && opts.verify);
    return async (envelope) => {
      const checked = verify ? await refileGone(envelope, listLive) : envelope;
      return checked && stampEnvelope(checked, resolveId);
    };
  }

  /**
   * refileGone files an envelope whose profile is gone from the live list
   * into the default one, with a warning, and answers null when no profile
   * exists: it is held like one that names none. An envelope whose profile
   * is there, or that nobody can check (no live list), is returned as it
   * is; the bridge checks it again before it writes.
   */
  async function refileGone(envelope, listLive) {
    const meta = (envelope && envelope.meta) || {};
    if (!isValidProfileId(meta.profile)) return envelope;
    const named = meta.profile.trim();
    const live = await listLive();
    if (live.offline || live.profiles.some((p) => p.id === named)) return envelope;
    const fallback = defaultProfile(live.profiles);
    if (!fallback) return null;
    const warning = `the profile "${named}" no longer exists: filed into ${fallback.name} instead`;
    return Object.assign({}, envelope, {
      meta: applyToMeta(Object.assign({}, meta), fallback.id),
      warnings: ((envelope && envelope.warnings) || []).concat([warning]),
    });
  }

  /**
   * applyToMeta stamps the profile onto a finished capture's meta, exactly
   * as MonoCaptureForm.applyToMeta stamps the note. An absent or unusable
   * id leaves `profile` off the envelope entirely, which is what every
   * capture before this feature looked like.
   */
  function applyToMeta(meta, id) {
    const target = meta || {};
    if (isValidProfileId(id)) target.profile = id.trim();
    else delete target.profile;
    return target;
  }

  root.MonoCaptureProfile = {
    isValidProfileId, normalizeProfiles, defaultProfile, choose,
    load, remember, cacheProfiles, fetchProfiles, refresh, sticky, stickyOrAsk, applyToMeta,
    profileForSending, stampEnvelope, preparer,
    PROFILE_KEY, PROFILES_KEY, METHOD, LIST_TIMEOUT_MS, RECHECK_MS,
  };
})(globalThis);
