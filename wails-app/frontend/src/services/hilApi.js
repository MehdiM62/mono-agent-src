// The ONE place the bubbles talk to the human-in-loop queue (`monoagentcli
// hil ...`). AskCard, useHilAsks and the dock only call these functions, so
// when the send-approval backend settles its CLI/binding names, this file
// (and the three api.hil* lines it calls) is all to rewire. Every function
// resolves to data or {error}; none throws.
import { api } from './api.js'
import { normalizeAsks, safeUrl } from '../lib/askModel.js'

// listAsks returns the profile's pending asks (workflow HIL and send items),
// normalized (lib/askModel.js). A failed read is an empty list, not an
// error: the next poll tries again.
export async function listAsks() {
  try {
    return normalizeAsks(await api.hilList())
  } catch {
    return []
  }
}

// resolve approves or rejects ask `id`. approve: true with editText sends
// the edited text instead of the prepared one (send items); data holds the
// edited fields of a workflow HIL item. Resolves {ok:true,state} or {error}.
export async function resolve(id, { approve, editText, data } = {}) {
  if (!id) return { error: 'no id' }
  try {
    const res = await api.hilResolve(id, { approve: !!approve, editText, data })
    if (res?.error) return { error: res.error }
    return { ok: true, state: res?.state || (approve ? 'approved' : 'rejected') }
  } catch (err) {
    return { error: String(err?.message || err) }
  }
}

// attachment returns attachment n of send item id for an inline preview:
// {dataUrl, mime, name, size} or {error}.
export async function attachment(id, n) {
  try {
    const res = await api.hilAttachment(id, n)
    return res && !res.error ? res : { error: res?.error || 'unavailable' }
  } catch (err) {
    return { error: String(err?.message || err) }
  }
}

// openPath opens a local file in the OS viewer; openUrl a web link in the
// browser (http/https only).
export async function openPath(path) {
  if (!path) return false
  try { await api.openPathWithOS(path); return true } catch { return false }
}

export async function openUrl(url) {
  const safe = safeUrl(url)
  if (!safe) return false
  try { await api.openURL(safe); return true } catch { return false }
}
