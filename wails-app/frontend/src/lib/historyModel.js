// Pure rules for the unified history list (chats and org runs, one row each):
// how a row's status merges with an open bubble's live summary, how times and
// filters read, and how a delete result splits into outcomes.

export const PAGE_SIZE = 30
export const ACCESS_LEVELS = ['scoped', 'coding', 'workspace', 'full']
export const KIND_FILTERS = ['chat', 'org']
export const STATUS_FILTERS = ['running', 'needs', 'error', 'done', 'idle']

// bubbleKeyOf is the key the bubble store uses for a row: a chat's key is its
// conversation id, an org's is org:<name> (per-run bubbles are wave U5).
export function bubbleKeyOf(row) {
  return row.kind === 'org' ? `org:${row.orgName}` : row.id
}

// liveStatus overlays an open bubble's live summary on the stored row. Only
// states the stored row cannot know yet (a turn running now, a question
// waiting) override it; unread counts ride along.
export function liveStatus(row, summary) {
  const base = { status: row.status || 'idle', unread: 0, needs: 0 }
  if (!summary) return base
  const out = { ...base, unread: summary.unread || 0, needs: summary.needs || 0 }
  if (summary.status === 'working') out.status = 'running'
  else if (summary.status === 'needs') out.status = 'needs'
  return out
}

// relativeTime: "just now", "5m ago", "3h ago", "2d ago", else the date.
export function relativeTime(iso, now = Date.now()) {
  const t = Date.parse(iso)
  if (!iso || Number.isNaN(t)) return ''
  const s = Math.max(0, Math.round((now - t) / 1000))
  if (s < 45) return 'just now'
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))}m ago`
  if (s < 86400) return `${Math.round(s / 3600)}h ago`
  if (s < 7 * 86400) return `${Math.round(s / 86400)}d ago`
  return new Date(t).toISOString().slice(0, 10)
}

// toFilter is the binding's filter object; linked is applied client-side
// because the backend only matches one exact application id.
export function toFilter({ kind = '', status = '', query = '' } = {}) {
  return { kind, status, application: '', query: query.trim() }
}

export function applyLinked(items, linked) {
  return linked ? items.filter(r => r.applicationId) : items
}

// mergePage appends a page, dropping ids already shown (a refresh can shift rows).
export function mergePage(existing, page) {
  const seen = new Set(existing.map(r => r.id))
  return [...existing, ...page.filter(r => !seen.has(r.id))]
}

// deleteOutcome splits DeleteChatConversations' answer: ids gone, ids kept
// because a turn is running, and ids that failed with the CLI's message.
export function deleteOutcome(res) {
  return {
    deleted: res?.deleted || [],
    active: res?.active || [],
    failed: Object.entries(res?.failed || {}).map(([id, message]) => ({ id, message })),
  }
}

// chatIdsOnly guards every delete path: org runs are never deleted from here.
export function chatIdsOnly(rows) {
  return rows.filter(r => r.kind === 'chat').map(r => r.id)
}
