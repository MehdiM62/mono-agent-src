// The ONE place the GUI learns how the CLI describes a communication's
// approval state (#479). person_messages rows carry `status` and `hil_ref`
// (the HIL item id); people carry `kind` ('person' | 'company'). If the
// backend renames any of it, change the accessors below and nothing else.

export const STATUSES = ['pending_approval', 'approved', 'sending', 'sent', 'rejected', 'failed', 'expired']

// Filter chips on the Communications page: key -> the statuses it shows.
export const STATUS_FILTERS = [
  { key: 'all', statuses: null },
  { key: 'pending_approval', statuses: ['pending_approval'] },
  { key: 'approved', statuses: ['approved', 'sending'] },
  { key: 'sent', statuses: ['sent'] },
  { key: 'rejected', statuses: ['rejected'] },
  { key: 'failed', statuses: ['failed', 'expired'] },
]

// Colour AND a glyph: the badge never relies on colour alone.
const META = {
  pending_approval: { color: '#f59e0b', glyph: '⏳' },
  approved:         { color: '#00b4d8', glyph: '✓' },
  sending:          { color: '#00b4d8', glyph: '→' },
  sent:             { color: '#22c55e', glyph: '✓✓' },
  rejected:         { color: '#ef4444', glyph: '✕' },
  failed:           { color: '#ef4444', glyph: '!' },
  expired:          { color: '#94a3b8', glyph: '–' },
}

export const statusMeta = (status) => META[status] || META.sent

// Only outbound rows have an approval state; inbound ones get none. Rows
// from before the field existed are 'sent'.
export function messageStatus(msg) {
  if (!msg || msg.direction === 'inbound') return null
  return STATUSES.includes(msg.status) ? msg.status : 'sent'
}

export const isPending = (msg) => messageStatus(msg) === 'pending_approval'
export const hilRefOf = (msg) => (msg && msg.hil_ref) || ''

export function matchesStatusFilter(msg, key) {
  const f = STATUS_FILTERS.find(x => x.key === key)
  if (!f || !f.statuses) return true
  return f.statuses.includes(messageStatus(msg))
}

// The status history, when the row's metadata keeps one:
// metadata.status_history = [{status, at}, ...] (object or JSON string).
// Falls back to the current status alone.
export function statusTimeline(msg) {
  let meta = msg?.metadata
  if (typeof meta === 'string') { try { meta = JSON.parse(meta) } catch { meta = null } }
  const hist = meta && (meta.status_history || meta.timeline)
  if (Array.isArray(hist) && hist.length) {
    return hist
      .map(h => ({ status: h.status || h.state, at: h.at || h.time || h.ts || '' }))
      .filter(h => h.status)
  }
  const s = messageStatus(msg)
  return s ? [{ status: s, at: msg.sent_at || msg.created_at || '' }] : []
}

// People: kind, own account, and the communications of one person.
export const isCompany = (p) => p?.kind === 'company'
export const isOwnAccount = (p) => !!(p && (p.is_own_account || p.own_account || p.is_self))

// commsByPerson: person id -> { total, pending } from the messages list.
export function commsByPerson(messages) {
  const out = {}
  for (const m of messages || []) {
    if (!m.person_id) continue
    const e = out[m.person_id] || (out[m.person_id] = { total: 0, pending: 0 })
    e.total += 1
    if (isPending(m)) e.pending += 1
  }
  return out
}

// The person and HIL item an ask (lib/askModel.js) points at.
export const askPersonId = (ask) => ask?.recipient?.personId || ask?.personId || ask?.person_id || ''
export const askHilRef = (ask) => ask?.hilRef || ask?.hil_ref || (ask?.kind === 'send' ? ask.id : '') || ''

// A decision on a HIL item changes the Communications row it belongs to;
// Communications (kept mounted by the app) reloads when it hears this.
const COMMS_CHANGED = 'monoagent:comms-changed'
export function emitCommsChanged() {
  window.dispatchEvent(new CustomEvent(COMMS_CHANGED))
}
export function onCommsChanged(cb) {
  window.addEventListener(COMMS_CHANGED, cb)
  return () => window.removeEventListener(COMMS_CHANGED, cb)
}
