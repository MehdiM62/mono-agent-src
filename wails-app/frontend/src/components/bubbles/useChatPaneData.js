import { useEffect, useState } from 'react'
import { api } from '../../services/api.js'
import { cachedAgentScan } from '../../lib/agentRuntimes.js'
import { accessOfConversation } from '../../lib/chatAccess.js'
import { aitaskBudgetApi } from '../settings/aitaskBudgetApi.js'

// What the pane's settings popover reads about one open conversation, loaded
// when `active` (the popover is open). Each part fails on its own: a missing
// part reads as null and the popover says so.

// useBrowserLevel: the level for an agent kind from `browser permissions`
// ({ level, label, isDefault }), or null.
export function useBrowserLevel(kind, active) {
  const [level, setLevel] = useState(undefined)
  useEffect(() => {
    if (!active) return undefined
    let live = true
    Promise.resolve().then(() => api.browserPermissions())
      .then(res => {
        if (!live) return
        const row = (res?.kinds || []).find(k => k.kind === kind)
        const label = (res?.catalog || []).find(c => c.id === row?.level)?.label || row?.level
        setLevel(row ? { level: row.level, label, isDefault: !!row.isDefault } : null)
      })
      .catch(() => { if (live) setLevel(null) })
    return () => { live = false }
  }, [kind, active])
  return level
}

// useCostVsCap: { spentUsd, capUsd, scope } for a conversation bound to a task
// (that task's cap), else the global cap from the budget list; null when none
// is set or it cannot be read.
export function useCostVsCap(taskId, active) {
  const [cap, setCap] = useState(undefined)
  useEffect(() => {
    if (!active) return undefined
    let live = true
    const read = taskId
      ? aitaskBudgetApi.forTask(taskId).then(b => (b?.capUsd > 0 ? { spentUsd: b.reported ? b.spentUsd : null, capUsd: b.capUsd, scope: 'task' } : null))
      : aitaskBudgetApi.list().then(b => {
        const global = (b?.caps || []).find(c => c.scope === 'global')
        return global && global.capUsd > 0 ? { spentUsd: global.usedUsd ?? b.spentUsd ?? null, capUsd: global.capUsd, scope: 'global' } : null
      })
    read.then(v => { if (live) setCap(v) }).catch(() => { if (live) setCap(null) })
    return () => { live = false }
  }, [taskId, active])
  return cap
}

// useResolveAccess reads a restored or task-opened bubble's access level when
// it carries none (a bare conversation id), from the conversation list, a few
// pages deep. The level it finds is stored on the bubble.
export function useResolveAccess(bubble, store) {
  const { key, conversationId, access } = bubble
  useEffect(() => {
    if (!conversationId || access) return undefined
    let live = true
    ;(async () => {
      let cursor = ''
      for (let page = 0; page < 3 && live; page++) {
        // eslint-disable-next-line no-await-in-loop
        const res = await api.listChatConversations(cursor, 50)
        const hit = (res?.items || []).find(c => c.id === conversationId)
        if (hit) {
          if (live) store.patchBubble(key, { access: accessOfConversation(hit), orgMode: hit.orgMode === 'dynamic' ? 'dynamic' : '' })
          return
        }
        cursor = res?.nextCursor || ''
        if (!cursor) return
      }
    })().catch(() => {})
    return () => { live = false }
  }, [key, conversationId, access, store])
}

// useStreamsIncrementally: whether a scoped chat's runtime streams its reply as
// it goes (the scan entry's streams_incrementally), which decides what the turn
// status says while it waits. True until the scan says otherwise.
export function useStreamsIncrementally(runtime, active) {
  const [streams, setStreams] = useState(true)
  useEffect(() => {
    if (!active || !runtime) return undefined
    let live = true
    Promise.resolve().then(() => cachedAgentScan())
      .then(res => { if (live) setStreams((res?.agents || []).find(a => a.id === runtime)?.streams_incrementally !== false) })
      .catch(() => {})
    return () => { live = false }
  }, [runtime, active])
  return streams
}
