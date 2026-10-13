import { useCallback, useEffect, useRef, useState } from 'react'
import * as hil from '../../services/hilApi.js'
import { asksFor } from '../../lib/askModel.js'

export const HIL_POLL_MS = 4000

// useHilAsks is an expanded bubble's workflow-HIL and send-approval asks:
// `hil list` read on open and every few seconds while the window is visible,
// narrowed to this bubble (asksFor), plus resolve(). A resolved ask stays in
// the list with its outcome until the bubble closes, so the person sees how
// it ended instead of the card vanishing.
//   scope  { org, conversationId, roleIds }  see lib/askModel.js asksFor
// Every action goes through services/hilApi.js; nothing here decides whether
// a send may go out.
export function useHilAsks(scope, enabled = true) {
  const [asks, setAsks] = useState([])
  const [busy, setBusy] = useState({})
  const [outcome, setOutcome] = useState({})
  const keptRef = useRef({})
  const busyRef = useRef({})
  const live = useRef(true)
  const scopeKey = JSON.stringify({ org: scope?.org || '', conversationId: scope?.conversationId || '', roleIds: scope?.roleIds || [] })

  const refresh = useCallback(async () => {
    const all = await hil.listAsks()
    if (!live.current) return
    const mine = asksFor(all, JSON.parse(scopeKey))
    // Asks resolved here stay, as resolved, until the list drops them.
    const ids = new Set(mine.map(a => a.id))
    const kept = Object.values(keptRef.current).filter(a => !ids.has(a.id))
    setAsks([...mine, ...kept])
  }, [scopeKey])

  useEffect(() => {
    live.current = true
    if (!enabled) return () => { live.current = false }
    refresh()
    const iv = setInterval(() => { if (!document.hidden) refresh() }, HIL_POLL_MS)
    return () => { live.current = false; clearInterval(iv) }
  }, [enabled, refresh])

  const resolve = useCallback(async (ask, approve, extras = {}) => {
    if (!ask?.id || busyRef.current[ask.id]) return null
    busyRef.current = { ...busyRef.current, [ask.id]: true }
    setBusy(busyRef.current)
    try {
      const res = await hil.resolve(ask.id, { approve, editText: extras.editText, data: extras.data })
      setOutcome(o => ({ ...o, [ask.id]: res.error ? { error: res.error } : { state: res.state } }))
      if (!res.error) {
        keptRef.current = { ...keptRef.current, [ask.id]: { ...ask, pending: false, state: res.state } }
        refresh()
      }
      return res
    } finally {
      const next = { ...busyRef.current }
      delete next[ask.id]
      busyRef.current = next
      if (live.current) setBusy(next)
    }
  }, [refresh])

  return { asks, busy, outcome, resolve, refresh }
}
