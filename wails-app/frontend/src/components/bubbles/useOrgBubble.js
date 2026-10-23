import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api, onOrgEvent } from '../../services/api.js'
import { initialOrgBubble, applyOrgBusEvent, touchesThread } from '../../lib/orgBubble.js'

export const REFRESH_MS = 700

function logItems(res) {
  if (!res || res.error) return []
  if (Array.isArray(res.items)) return res.items
  if (Array.isArray(res.events)) return res.events
  return Array.isArray(res) ? res : []
}

// useOrgBubble is an expanded org bubble's live state (#229):
//   history — `org chat history` (the boss thread, the roles, the status),
//             fetched on open and again after any bus event that can change
//             the thread (debounced, never two fetches at once);
//   bubble  — the running-org adapter (lib/orgBubble.js) seeded from the
//             current run's log, then folded live from `org:event`;
//   echoes  — messages the person sent that the thread doesn't show yet;
//   busy / outcome — per item ref: a resolve in flight (its buttons are
//             disabled, so nothing is sent twice), and how it ended.
// Every action goes through a CLI-backed binding and resolves to its JSON
// or {error}; nothing here decides whether an org may be answered.
//
// A finished run (readOnly, with its runId) reads that run's thread and log,
// ignores the bus, and refuses every write: it is a record, not a session.
export function useOrgBubble(orgName, { runId = '', readOnly = false } = {}) {
  const histRun = readOnly ? runId : ''
  const [history, setHistory] = useState(null)
  const [historyError, setHistoryError] = useState('')
  const [bubble, setBubble] = useState(null)
  const [echoes, setEchoes] = useState([])
  const [sending, setSending] = useState(false)
  const [sendError, setSendError] = useState('')
  const [busy, setBusy] = useState({})
  const [outcome, setOutcome] = useState({})
  const [controlling, setControlling] = useState('')
  const live = useRef(true)
  const fetching = useRef(false)
  const dirty = useRef(false)
  const timer = useRef(null)
  const busyRef = useRef({})

  const refresh = useCallback(async () => {
    if (fetching.current) { dirty.current = true; return }
    fetching.current = true
    try {
      do {
        dirty.current = false
        const res = await api.getOrgChatHistory(orgName, histRun)
        if (!live.current) return
        if (res?.error) setHistoryError(res.error)
        else { setHistoryError(''); setHistory(res) }
      } while (dirty.current && live.current)
    } finally {
      fetching.current = false
    }
  }, [orgName, histRun])

  const scheduleRefresh = useCallback(() => {
    clearTimeout(timer.current)
    timer.current = setTimeout(refresh, REFRESH_MS)
  }, [refresh])

  useEffect(() => {
    live.current = true
    refresh()
    return () => { live.current = false; clearTimeout(timer.current) }
  }, [refresh])

  // The stage: seeded from the run's log once the roles are known; live
  // events that arrive before that are buffered and folded after it, so the
  // order matches the bus.
  const boss = history?.boss || ''
  const rolesKey = JSON.stringify(history?.roles || [])
  const ready = !!history
  const buffered = useRef([])
  const seeded = useRef(false)
  useEffect(() => {
    const off = onOrgEvent(payload => {
      if (readOnly || payload?.orgName !== orgName || !payload.event) return
      if (touchesThread(payload.event, boss)) scheduleRefresh()
      if (!seeded.current) { buffered.current.push(payload.event); return }
      setBubble(prev => (prev ? applyOrgBusEvent(prev, payload.event) : prev))
    })
    return off
  }, [orgName, boss, scheduleRefresh, readOnly])

  useEffect(() => {
    if (!ready) return undefined
    let cancelled = false
    seeded.current = false
    Promise.resolve().then(() => api.getOrgLogs(orgName, histRun)).catch(() => null).then(res => {
      if (cancelled) return
      let s = initialOrgBubble({ org: orgName, boss, roles: JSON.parse(rolesKey), prejoin: true })
      for (const ev of logItems(res)) s = applyOrgBusEvent(s, ev)
      for (const ev of buffered.current) s = applyOrgBusEvent(s, ev)
      buffered.current = []
      seeded.current = true
      setBubble(s)
    })
    return () => { cancelled = true }
  }, [ready, orgName, boss, rolesKey, histRun])

  // An echo stays until the thread shows the person's message.
  const items = useMemo(() => history?.items || [], [history])
  useEffect(() => {
    if (!echoes.length) return
    const shown = new Set(items.filter(it => it.kind === 'human').map(it => it.text.trim()))
    const rest = echoes.filter(e => !shown.has(e.text.trim()))
    if (rest.length !== echoes.length) setEchoes(rest)
  }, [items, echoes])

  const send = useCallback(async (text) => {
    const body = String(text || '').trim()
    if (!body || sending || readOnly) return false
    setSending(true)
    setSendError('')
    try {
      const res = await api.sendOrgChat(orgName, body)
      if (res?.error) { setSendError(res.error); return false }
      setEchoes(list => [...list, { id: res.messageId || `echo-${Date.now()}`, text: body, delivery: res.delivery || '' }])
      scheduleRefresh()
      return true
    } finally {
      setSending(false)
    }
  }, [orgName, sending, scheduleRefresh, readOnly])

  // resolveItem answers a question (answer given), dismisses one (dismiss,
  // note is the reason) or approves/denies an approval or gate. A ref already in flight is ignored.
  const resolveItem = useCallback(async (ref, { answer, dismiss = false, approve = false, note = '' } = {}) => {
    if (!ref || readOnly || busyRef.current[ref]) return null
    busyRef.current = { ...busyRef.current, [ref]: true }
    setBusy(busyRef.current)
    try {
      let res
      if (dismiss) res = await api.dismissOrgChat(orgName, ref, note)
      else if (answer != null) res = await api.answerOrgChat(orgName, ref, answer)
      else res = await api.resolveOrgChat(orgName, ref, approve, note)
      setOutcome(o => ({ ...o, [ref]: res?.error ? { error: res.error } : { state: res.state, already: !!res.already } }))
      if (!res?.error) scheduleRefresh()
      return res
    } finally {
      const next = { ...busyRef.current }
      delete next[ref]
      busyRef.current = next
      setBusy(next)
    }
  }, [orgName, scheduleRefresh, readOnly])

  const control = useCallback(async (verb) => {
    if (controlling || readOnly) return null
    setControlling(verb)
    try {
      const res = await api.controlOrg(orgName, verb)
      scheduleRefresh()
      return res
    } finally {
      setControlling('')
    }
  }, [orgName, controlling, scheduleRefresh, readOnly])

  return {
    history, historyError, items, bubble, echoes, sending, sendError, busy, outcome, controlling,
    running: !readOnly && history?.status === 'running', paused: !readOnly && !!history?.paused, readOnly,
    send, resolveItem, control, refresh,
  }
}
