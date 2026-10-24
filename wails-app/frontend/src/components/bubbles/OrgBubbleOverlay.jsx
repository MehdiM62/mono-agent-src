import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Minimize2, X, ChevronUp, ChevronDown, Pause, Play, Square, Network, ExternalLink } from 'lucide-react'
import { confirm } from '../ConfirmDialog.jsx'
import { notify } from '../../services/api.js'
import { shouldCollapseOnBackdrop } from '../../lib/coderBubbles.js'
import { orgStageOf } from '../../lib/orgBubble.js'
import { LEAD_ID } from '../../lib/orgStage.js'
import { runtimeLabel } from '../../lib/runtimeLabels.js'
import { OrgStage } from '../stage/OrgStage.jsx'
import { StageDrawer } from '../stage/StageDrawer.jsx'
import { useRolesAccess } from '../orgdesigner/fullAccess.jsx'
import { OrgChatView } from './OrgChatView.jsx'
import { useOrgBubble } from './useOrgBubble.js'
import './bubbles.css'
import { isModalOpenAbove } from './modalAbove.js'
import './asks.css'

const mono = 'var(--font-mono)'
const CLOSE_MS = 170

function isModalOpen() {
  return isModalOpenAbove()
}

function prefersReducedMotion() {
  return typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
}

// ThreadTabs switches the chat between the boss thread, the team's
// role-to-role timeline and (while a role is selected on the stage) that
// role's thread. Arrow keys move between the tabs.
function ThreadTabs({ view, roleName, onView }) {
  const { t } = useTranslation()
  const tabs = [
    { id: 'boss', label: t('orgBubble.threadBoss'), go: () => onView({ mode: 'boss' }) },
    { id: 'team', label: t('orgBubble.threadTeam'), go: () => onView({ mode: 'team' }) },
    ...(roleName ? [{ id: 'role', label: roleName, go: () => {} }] : []),
  ]
  const onKey = (e) => {
    const i = tabs.findIndex(x => x.id === view.mode)
    const step = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0
    if (!step) return
    e.preventDefault()
    const next = tabs[(i + step + tabs.length) % tabs.length]
    next.go()
    requestAnimationFrame(() => document.querySelector(`[data-thread-tab="${next.id}"]`)?.focus())
  }
  return (
    <div className="org-thread-tabs" role="tablist" aria-label={t('orgBubble.threadsLabel')} onKeyDown={onKey}>
      {tabs.map(tab => (
        <button key={tab.id} type="button" role="tab" data-thread-tab={tab.id} aria-selected={view.mode === tab.id}
          tabIndex={view.mode === tab.id ? 0 : -1} className={view.mode === tab.id ? 'active' : ''} onClick={tab.go}>
          {tab.label}
        </button>
      ))}
    </div>
  )
}

// OrgBubbleOverlay is an expanded running-org bubble (#229): the org's
// stage on top (lib/orgBubble.js over its bus), and the chat with its boss
// below, over a dimmed backdrop. It closes back into its bubble the way a
// coder bubble does (backdrop, Esc, the collapse button), and collapsing
// never touches the org. Pause, resume and stop each ask first. Full
// access shows as badges; granting it stays on the Orgs page.
export function OrgBubbleOverlay({ bubble, store, originRect, onCollapse, onClose, onNavigate }) {
  const { t } = useTranslation()
  const orgName = bubble.orgName
  const view = store.getView(bubble.key)
  const [closing, setClosing] = useState(false)
  const [narrow, setNarrow] = useState(() => typeof window !== 'undefined' && window.innerWidth < 640)
  const [stageRatio, setStageRatio] = useState(view.stageRatio ?? 0.4)
  const [stageCollapsed, setStageCollapsed] = useState(!!view.stageCollapsed)
  const [draft, setDraft] = useState(view.draft || '')
  const [selected, setSelected] = useState(null)
  const [teamView, setTeamView] = useState(false)
  const selectedRef = useRef(null)
  const bodyRef = useRef(null)
  const panelRef = useRef(null)
  const pressOnBackdrop = useRef(false)
  const finished = !!bubble.finished
  const conv = useOrgBubble(orgName, { runId: bubble.runId, readOnly: finished })
  const { byRole: access } = useRolesAccess(orgName)

  useEffect(() => { store.setView(bubble.key, { draft }) }, [store, bubble.key, draft])
  useEffect(() => { store.setView(bubble.key, { stageRatio, stageCollapsed }) }, [store, bubble.key, stageRatio, stageCollapsed])

  useEffect(() => {
    const onResize = () => setNarrow(window.innerWidth < 640)
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  const collapse = useCallback(() => {
    if (closing) return
    if (prefersReducedMotion()) { onCollapse(); return }
    setClosing(true)
    setTimeout(onCollapse, CLOSE_MS)
  }, [closing, onCollapse])

  useEffect(() => {
    const onKey = (e) => {
      if (e.key !== 'Escape' || isModalOpen()) return
      e.stopImmediatePropagation()
      if (selectedRef.current) { setSelected(null); return }
      collapse()
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [collapse])

  useLayoutEffect(() => {
    const el = panelRef.current
    if (!el || !originRect) return
    const box = el.getBoundingClientRect()
    el.style.setProperty('--origin-x', `${originRect.left + originRect.width / 2 - box.left}px`)
    el.style.setProperty('--origin-y', `${originRect.top + originRect.height / 2 - box.top}px`)
  }, [originRect])

  // Focus goes into the dialog at once and into the composer once it can be
  // typed in: it stays disabled until the thread has loaded (#477). Focus the
  // person moved in the meantime is left where it is. Given back on close.
  useEffect(() => {
    const prev = document.activeElement
    const panel = panelRef.current
    const ta = panel?.querySelector('textarea:not([disabled])')
    if (ta) ta.focus()
    else panel?.focus?.()
    return () => prev?.focus?.()
  }, [])
  const loaded = !!conv.history
  useEffect(() => {
    const panel = panelRef.current
    if (!loaded || !panel || document.activeElement !== panel) return
    panel.querySelector('textarea:not([disabled])')?.focus?.()
  }, [loaded])

  const startDrag = (e) => {
    e.preventDefault()
    const body = bodyRef.current?.getBoundingClientRect()
    if (!body) return
    const onMove = (ev) => setStageRatio(Math.min(0.7, Math.max(0.15, (ev.clientY - body.top) / body.height)))
    const onUp = () => { window.removeEventListener('pointermove', onMove); window.removeEventListener('pointerup', onUp) }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
  }
  const onDividerKey = (e) => {
    if (e.key === 'ArrowUp') { e.preventDefault(); setStageRatio(r => Math.max(0.15, r - 0.05)) }
    if (e.key === 'ArrowDown') { e.preventDefault(); setStageRatio(r => Math.min(0.7, r + 0.05)) }
  }

  const stage = useMemo(() => (conv.bubble ? orgStageOf(conv.bubble, access) : null), [conv.bubble, access])
  const selectedNode = selected ? (stage?.nodes?.[selected] || null) : null
  selectedRef.current = selectedNode ? selected : null
  const selectNode = useCallback(id => { setTeamView(false); setSelected(cur => (cur === id ? null : id)) }, [])

  // What the chat shows: the boss thread by default, one role's slice when a
  // role is selected on the stage, or the team's role-to-role timeline.
  const threadMode = useMemo(() => {
    if (selectedNode && selected !== LEAD_ID) return { mode: 'role', role: selected }
    return teamView ? { mode: 'team' } : { mode: 'boss' }
  }, [selectedNode, selected, teamView])
  const setThreadMode = useCallback(next => {
    setSelected(null)
    setTeamView(next?.mode === 'team')
  }, [])

  const history = conv.history
  const roleTitles = useMemo(() => Object.fromEntries((history?.roles || []).map(r => [r.id, r.title || r.id])), [history])
  const nameOf = useCallback(id => roleTitles[id] || id || t('orgBubble.boss'), [roleTitles, t])
  const boss = (history?.roles || []).find(r => r.id === history?.boss)
  const leadInfo = { runtime: boss?.runtime || '', model: boss?.model || '', effort: '' }

  const status = finished ? 'finished' : history?.status || ''
  const running = status === 'running'
  const paused = running && !!history?.paused
  const statusKey = finished ? 'finished' : paused ? 'paused' : running ? 'running' : status === 'never run' ? 'neverRun' : status === 'crashed' ? 'crashed' : status ? 'stopped' : 'loading'

  const control = async (verb) => {
    const ok = await confirm(t(`orgBubble.confirm.${verb}Body`, { name: orgName }), {
      title: t(`orgBubble.confirm.${verb}Title`, { name: orgName }),
      confirmLabel: t(`orgBubble.${verb}`), cancelLabel: t('orgBubble.cancel'), danger: verb === 'stop',
    })
    if (!ok) return
    const res = await conv.control(verb)
    if (res?.error) notify('orgs', t('orgBubble.couldNotControl', { error: res.error }))
  }

  return (
    <>
      <div className={`bubble-backdrop${closing ? ' closing' : ''}`} data-testid="bubble-backdrop"
        onPointerDown={e => { pressOnBackdrop.current = e.target === e.currentTarget }}
        onClick={e => {
          const collapseIt = e.target === e.currentTarget && shouldCollapseOnBackdrop({
            pressStartedOnBackdrop: pressOnBackdrop.current,
            selectionText: window.getSelection?.()?.toString() || '',
            modalOpen: isModalOpen(),
          })
          pressOnBackdrop.current = false
          if (collapseIt) collapse()
        }} />
      <div ref={panelRef} role="dialog" tabIndex={-1} aria-label={t('orgBubble.chatLabel', { name: orgName })} data-testid="org-bubble-overlay"
        className={`bubble-overlay${closing ? ' closing' : ''}${narrow ? ' narrow' : ''}`}>
        {/* One row at any width (#477): the names shrink and cut off, the
            buttons keep their size, and narrow they are icons alone. */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '9px 12px', borderBottom: '1px solid var(--border)', flexShrink: 0, flexWrap: 'nowrap', minWidth: 0 }}>
          <Network size={13} style={{ color: 'var(--cyan)', flexShrink: 0 }} aria-hidden="true" />
          <span style={{ fontFamily: 'var(--font-display)', fontSize: 13, fontWeight: 600, color: 'var(--text)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', minWidth: 0 }}>{orgName}</span>
          <span data-testid="org-bubble-status" data-status={statusKey} title={statusKey === 'crashed' ? history?.status_error || undefined : undefined}
            style={{ fontFamily: mono, fontSize: 9.5, color: statusKey === 'finished' ? 'var(--text-muted)' : running && !paused ? 'var(--teal, #00f5d4)' : statusKey === 'crashed' ? 'var(--red, #ef4444)' : 'var(--text-muted)', border: '1px solid var(--border-bright)', borderRadius: 999, padding: '1px 7px', flexShrink: 0, whiteSpace: 'nowrap' }}>
            {t(`orgBubble.status.${statusKey}`)}
          </span>
          {bubble.runId && (
            <span data-testid="org-bubble-run" title={bubble.runId} style={{ fontFamily: mono, fontSize: 9.5, color: 'var(--text-muted)', flexShrink: 0, whiteSpace: 'nowrap' }}>
              {t('orgBubble.runLabel', { id: bubble.runId.slice(0, 8) })}
            </span>
          )}
          {(boss?.runtime || boss?.model) && (
            <span style={{ fontFamily: mono, fontSize: 9.5, color: 'var(--cyan)', minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {t('orgBubble.bossIs', { name: boss.title || boss.id })} · {[boss.runtime && runtimeLabel(boss.runtime), boss.model].filter(Boolean).join(' · ')}
            </span>
          )}
          <span style={{ flex: 1 }} />
          {running && (paused ? (
            <button type="button" className="btn btn-ghost btn-sm" data-testid="org-bubble-resume" disabled={!!conv.controlling} onClick={() => control('resume')} aria-label={t('orgBubble.resume')} style={{ gap: 4, fontSize: 10, flexShrink: 0 }}>
              <Play size={11} />{!narrow && <> {t('orgBubble.resume')}</>}
            </button>
          ) : (
            <button type="button" className="btn btn-ghost btn-sm" data-testid="org-bubble-pause" disabled={!!conv.controlling} onClick={() => control('pause')} aria-label={t('orgBubble.pause')} style={{ gap: 4, fontSize: 10, flexShrink: 0 }}>
              <Pause size={11} />{!narrow && <> {t('orgBubble.pause')}</>}
            </button>
          ))}
          {running && (
            <button type="button" className="btn btn-ghost btn-sm" data-testid="org-bubble-stop" disabled={!!conv.controlling} onClick={() => control('stop')} aria-label={t('orgBubble.stop')} style={{ gap: 4, fontSize: 10, flexShrink: 0 }}>
              <Square size={10} />{!narrow && <> {t('orgBubble.stop')}</>}
            </button>
          )}
          {onNavigate && (
            <button type="button" className="btn btn-ghost btn-sm" onClick={() => onNavigate('orgs', { org: orgName, run: bubble.runId || undefined })} title={t('orgBubble.openInOrgs')} aria-label={t('orgBubble.openInOrgs')} style={{ padding: '3px 6px', flexShrink: 0 }}>
              <ExternalLink size={12} />
            </button>
          )}
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => setStageCollapsed(c => !c)}
            aria-expanded={!stageCollapsed} title={stageCollapsed ? t('bubbles.showOrg') : t('bubbles.hideOrg')} aria-label={t('bubbles.org')} style={{ gap: 4, fontSize: 10, flexShrink: 0 }}>
            {stageCollapsed ? <ChevronDown size={12} /> : <ChevronUp size={12} />}{!narrow && <> {t('bubbles.org')}</>}
          </button>
          <button type="button" className="btn btn-ghost btn-sm" onClick={collapse} title={t('bubbles.collapse')} aria-label={t('bubbles.collapse')} style={{ padding: '3px 6px', flexShrink: 0 }}>
            <Minimize2 size={13} />
          </button>
          <button type="button" className="btn btn-ghost btn-sm" onClick={onClose} title={t('orgBubble.close')} aria-label={t('orgBubble.close')} style={{ padding: '3px 6px', flexShrink: 0 }}>
            <X size={13} />
          </button>
        </div>
        <div ref={bodyRef} style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', position: 'relative' }}>
          {!stageCollapsed && (
            <>
              <div style={{ height: `${Math.round(stageRatio * 100)}%`, flexShrink: 0 }}>
                <OrgStage stage={stage} turnId={conv.bubble?.run || ''} working={running && !paused}
                  leadInfo={leadInfo} leadIdle={running ? t('orgBubble.bossIdle') : t('orgBubble.status.stopped')}
                  emptyHint={t('orgBubble.teamHint')}
                  selectedId={selected} onSelect={selectNode} />
              </div>
              <div className="bubble-divider" role="separator" aria-orientation="horizontal" tabIndex={0}
                aria-label={t('bubbles.resizeOrg')} aria-valuenow={Math.round(stageRatio * 100)} aria-valuemin={15} aria-valuemax={70}
                onPointerDown={startDrag} onKeyDown={onDividerKey} />
            </>
          )}
          <ThreadTabs view={threadMode} roleName={threadMode.mode === 'role' ? nameOf(threadMode.role) : ''} onView={setThreadMode} />
          <OrgChatView conv={conv} nameOf={nameOf} draft={draft} onDraftChange={setDraft} orgName={orgName}
            view={threadMode} onView={setThreadMode} onNavigate={onNavigate && ((page, params) => onNavigate(page, params))} />
          {selectedNode && (
            <StageDrawer node={selectedNode} calls={conv.bubble?.calls} leadInfo={leadInfo} turnId={conv.bubble?.run || ''} isLive={running}
              canStop={false} onClose={() => setSelected(null)} onNavigate={onNavigate} />
          )}
        </div>
      </div>
    </>
  )
}

