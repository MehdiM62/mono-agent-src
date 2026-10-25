// Pure logic of the mission cards and strip in a coder chat (plan section 9):
// which events become cards, which session belongs to a conversation, and
// what the strip shows. No React; the CLI decides what is allowed.

const TERMINAL = ['done', 'stopped', 'failed']

// isTerminal: a session that is over (or archived) gets no strip of its own.
export const isTerminal = (s) => !!s && (s.archived || TERMINAL.includes(s.state))

// missionItems turns a conversation's turns ([{key, state}], oldest first)
// into the cards each turn shows: every proposal, the latest plan per
// session and the latest report per session (a later event, in any turn,
// replaces an earlier one and its card). Returns {key: [{type, id, payload}]}.
export function missionItems(turns) {
  const latest = new Map() // `${type}:${sessionId}` -> turn key
  turns.forEach(({ key, state }) => {
    for (const e of state?.missions || []) {
      if (e.type !== 'mission.propose' && e.payload?.sessionId != null) latest.set(`${e.type}:${e.payload.sessionId}`, key)
    }
  })
  const out = {}
  turns.forEach(({ key, state }) => {
    const items = []
    const seen = new Set()
    const list = state?.missions || []
    list.forEach((e, i) => {
      const p = e.payload || {}
      if (e.type === 'mission.propose') {
        items.push({ type: 'propose', id: `${key}:${i}`, payload: p })
        return
      }
      if (p.sessionId == null) return
      const sig = `${e.type}:${p.sessionId}`
      if (latest.get(sig) !== key || seen.has(sig)) return
      // The last event of this turn for the session is the one shown.
      const last = list.map((x, j) => (`${x.type}:${x.payload?.sessionId}` === sig ? j : -1)).filter(j => j >= 0).pop()
      if (last !== i) return
      seen.add(sig)
      items.push({ type: e.type === 'mission.plan' ? 'plan' : 'report', id: sig, payload: p })
    })
    if (items.length) out[key] = items
  })
  return out
}

// sessionOf is the session of a conversation: the newest one still going,
// else the newest one that is not archived (a finished mission), else null.
export function sessionOf(sessions, conversationId) {
  if (!conversationId) return null
  const mine = (sessions || []).filter(s => s.conversation_id === conversationId && !s.archived)
  mine.sort((a, b) => b.id - a.id)
  return mine.find(s => !isTerminal(s)) || mine[0] || null
}

// stripFor says whether the strip shows for a session: while it is not over,
// and a finished one once its report arrived (the strip then reads Done).
export const stripFor = (session, reported) => !!session && (!isTerminal(session) || (session.state === 'done' && !!reported))

// workingOn lists the tasks in progress on a session's board: [{id, title, worker}].
export function workingOn(board) {
  const col = board?.tasks?.in_progress || board?.columns?.in_progress || []
  return col.map(t => ({ id: t.id, title: t.title, worker: t.assignee || t.claim?.by || '' }))
}

// stripActions: the buttons the strip offers (the CLI refuses the rest anyway).
export function stripActions(session) {
  if (!session || isTerminal(session)) return []
  if (session.state === 'running') return ['pause', 'stop']
  if (session.state === 'paused') return ['resume', 'stop']
  return ['stop'] // draft
}
