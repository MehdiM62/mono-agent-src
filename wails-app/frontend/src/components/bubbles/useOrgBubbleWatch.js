import { useCallback, useEffect, useRef } from 'react'
import { api, onOrgEvent, onOrgEventsClosed, onOrgRunStatus } from '../../services/api.js'
import { orgsToAutoOpen, isOrgStart } from '../../lib/orgBubble.js'
import { restartOrgEvents } from '../../lib/orgEventStreams.js'
import { isOrgBubble } from '../../lib/coderBubbles.js'
import { onOpenOrgBubble } from '../../lib/appEvents.js'
import { asksFor, pendingCount } from '../../lib/askModel.js'
import * as hil from '../../services/hilApi.js'

export const WATCH_MS = 30_000
// At most this many running orgs are asked for their pending items per poll.
const MAX_NEEDS_CHECKS = 6

function orgRows(res) {
  return Array.isArray(res?.orgs) ? res.orgs : []
}

// useOrgBubbleWatch keeps running orgs and their bubbles in step (#229, and
// the auto-dock of the coder-bubble v2 plan):
//   - every running org gets a bubble in the dock, collapsed, without opening
//     anything: at once when the app starts a run (org:runStatus) or a bus
//     tail sees `org started`, and within a poll for runs it can't see (a
//     schedule or daemon). A bubble the person closed stays closed for that
//     run unless more items wait than when it was closed; the next run docks
//     it again. One bubble per org, never two;
//   - an open org bubble's event tail is restarted when its org starts a
//     new run (or starts at all), since a tail follows the run it found;
//   - workflow-HIL and send-approval asks waiting for the person are counted
//     into each bubble's ring (store.setHilNeeds);
//   - "Open as bubble" anywhere (appEvents) opens one.
// It returns onCloseOrg for CoderBubbles.
export function useOrgBubbleWatch(store) {
  const dismissed = useRef({})
  const runs = useRef({})
  const closedTails = useRef({})
  const storeRef = useRef(store)
  storeRef.current = store

  useEffect(() => onOpenOrgBubble(({ org }) => { if (org) storeRef.current.openOrg(org) }), [])

  useEffect(() => onOrgEventsClosed(payload => {
    if (payload?.orgName) closedTails.current[payload.orgName] = true
  }), [])

  // A run the app starts, or one a held tail sees begin, docks its bubble
  // now. Starting a run on purpose is a new run: it overrides an earlier
  // close. A run ending forgets the close.
  useEffect(() => onOrgRunStatus(payload => {
    const org = payload?.orgName
    if (!org) return
    if (payload.status === 'running') {
      delete dismissed.current[org]
      storeRef.current.openOrg(org, { expand: false })
    } else if (payload.status === 'stopped' || payload.status === 'error') {
      delete dismissed.current[org]
    }
  }), [])

  useEffect(() => onOrgEvent(payload => {
    const org = payload?.orgName
    if (!org || !isOrgStart(payload.event)) return
    if (dismissed.current[org] != null) return
    storeRef.current.openOrg(org, { expand: false })
  }), [])

  useEffect(() => {
    let cancelled = false
    let inFlight = false
    const poll = async () => {
      if (inFlight || cancelled || (typeof document !== 'undefined' && document.hidden)) return
      inFlight = true
      try {
        const summary = await api.getOrgSummary(true)
        if (cancelled) return
        const rows = orgRows(summary)
        const bubbles = storeRef.current.bubbles
        const open = new Set(bubbles.filter(isOrgBubble).map(b => b.orgName))

        // Open bubbles: restart a tail whose org started a new run.
        for (const name of open) {
          const row = rows.find(r => r.name === name)
          if (!row?.running) continue
          const st = await api.getOrgStatus(name)
          if (cancelled) return
          const run = st?.run || ''
          if ((runs.current[name] !== undefined && runs.current[name] !== run) || closedTails.current[name]) {
            closedTails.current[name] = false
            restartOrgEvents(name)
          }
          runs.current[name] = run
        }

        // Running orgs without a bubble get one; does anything wait on the
        // person? Only the first few are asked.
        const checked = []
        let asked = 0
        for (const r of rows) {
          let needs = 0
          if (r.running && !open.has(r.name) && asked < MAX_NEEDS_CHECKS) {
            asked += 1
            const res = await api.listNeedsYou(r.name)
            if (cancelled) return
            needs = Array.isArray(res?.items) ? res.items.length : 0
          }
          checked.push({ name: r.name, running: !!r.running, needs })
        }
        const { open: toOpen, dismissed: next } = orgsToAutoOpen(checked, open, dismissed.current)
        dismissed.current = { ...Object.fromEntries(Object.entries(dismissed.current).filter(([k]) => !checked.some(c => c.name === k))), ...next }
        for (const name of toOpen) storeRef.current.openOrg(name, { expand: false })

        // Asks the bus doesn't carry: workflow HIL and send approvals.
        if (storeRef.current.setHilNeeds) {
          const asks = await hil.listAsks()
          if (cancelled) return
          const needs = {}
          for (const b of storeRef.current.bubbles) {
            const n = pendingCount(asksFor(asks, isOrgBubble(b) ? { org: b.orgName } : { conversationId: b.conversationId }))
            if (n > 0) needs[b.key] = n
          }
          storeRef.current.setHilNeeds(needs)
        }
      } catch {
        // The next poll tries again.
      } finally {
        inFlight = false
      }
    }
    const first = setTimeout(poll, 3_000)
    const iv = setInterval(poll, WATCH_MS)
    return () => { cancelled = true; clearTimeout(first); clearInterval(iv) }
  }, [])

  // Closing a bubble while its org runs keeps it closed for this run (until
  // more items wait than now). Closing one of a stopped org is forgotten
  // by the next poll.
  return useCallback((org, needs) => {
    if (org) dismissed.current = { ...dismissed.current, [org]: needs || 0 }
  }, [])
}
