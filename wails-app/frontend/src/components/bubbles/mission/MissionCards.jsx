import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, ListChecks, Square, Flag, ExternalLink } from 'lucide-react'
import { confirm } from '../../ConfirmDialog.jsx'
import { sessionsApi } from '../../../services/taskSessions.js'
import { isTerminal } from '../../../lib/missionModel.js'
import { useSessionLive } from './useMissionSession.js'
import './mission.css'

// The mission cards in a coder chat's transcript (plan section 9). All of
// them render what the CLI's session says; the buttons call the same Wails
// methods the Tasks tab does and the CLI decides whether they are allowed.

// declined proposals live here, per conversation, for the app session (UI state only).
const declinedByConv = new Map()
export const isDeclined = (conv, id) => !!declinedByConv.get(conv)?.has(id)
const decline = (conv, id) => {
  if (!declinedByConv.has(conv)) declinedByConv.set(conv, new Set())
  declinedByConv.get(conv).add(id)
}

export function ProposalCard({ item, conversationId, busy, started, onConfirm }) {
  const { t } = useTranslation()
  const [, force] = useState(0)
  const p = item.payload
  const gone = isDeclined(conversationId, item.id)
  return (
    <section className="ms-card" data-testid="mission-propose" aria-label={t('bubbles.mission.proposeLabel', { title: p.title })}>
      <div className="ms-card-head"><Flag size={12} aria-hidden="true" /> <strong>{p.title}</strong></div>
      {p.goal && <p className="ms-text">{p.goal}</p>}
      {p.reason && <p className="ms-muted">{p.reason}</p>}
      {started ? <p className="ms-muted" data-testid="mission-started">{t('bubbles.mission.started')}</p>
        : gone ? <p className="ms-muted" data-testid="mission-declined">{t('bubbles.mission.declined')}</p>
          : (
            <div className="ms-actions">
              <button type="button" className="btn btn-primary btn-sm" disabled={busy} onClick={() => onConfirm(p)}>{t('bubbles.mission.start')}</button>
              <button type="button" className="btn btn-ghost btn-sm" onClick={() => { decline(conversationId, item.id); force(n => n + 1) }}>{t('bubbles.mission.notNow')}</button>
            </div>
          )}
    </section>
  )
}

export function PlanCard({ item, onNavigate }) {
  const { t } = useTranslation()
  const p = item.payload
  const { session, missing, reload } = useSessionLive(p.sessionId)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const act = async (verb) => {
    if (verb === 'stop' && !(await confirm(t('tasks.session.confirmStop')))) return
    setBusy(true)
    try {
      const res = await sessionsApi[verb](p.sessionId)
      setError(res?.error || '')
      await reload()
    } finally { setBusy(false) }
  }
  const state = session?.state
  const draft = state === 'draft'
  const budget = Number(p.estimatedBudgetUsd) || 0
  return (
    <section className="ms-card" data-testid="mission-plan" aria-label={t('bubbles.mission.planLabel', { title: p.title })}>
      <div className="ms-card-head"><ListChecks size={12} aria-hidden="true" /> <strong>{p.title}</strong>
        {state && <span className={`ts-chip ts-chip--${state}`} data-testid="mission-plan-state">{t(`tasks.session.state.${state}`, { defaultValue: state })}</span>}
      </div>
      {p.goal && <p className="ms-text">{p.goal}</p>}
      <p className="ms-muted" data-testid="mission-plan-facts">
        {t('bubbles.mission.tasks', { count: p.taskCount || 0 })} · {t('bubbles.mission.estimate', { usd: budget.toFixed(2) })} · {t('bubbles.mission.round', { round: p.round || 0 })}
      </p>
      {missing && <p className="ms-muted" role="alert">{t('bubbles.mission.sessionGone')}</p>}
      {error && <p className="ms-error" role="alert">{error}</p>}
      <div className="ms-actions">
        {draft && <button type="button" className="btn btn-primary btn-sm" data-testid="mission-approve" disabled={busy} onClick={() => act('approve')}><Check size={12} aria-hidden="true" /> {t('bubbles.mission.approve')}</button>}
        {!missing && <button type="button" className="btn btn-secondary btn-sm" data-testid="mission-edit" onClick={() => onNavigate?.('tasks', { sessionId: p.sessionId })}><ExternalLink size={12} aria-hidden="true" /> {t('bubbles.mission.edit')}</button>}
        {draft && <button type="button" className="btn btn-ghost btn-sm" data-testid="mission-reject" disabled={busy} onClick={() => act('stop')}><Square size={12} aria-hidden="true" /> {t('bubbles.mission.reject')}</button>}
        {session && isTerminal(session) && <span className="ms-muted">{t(`tasks.session.state.${state}`, { defaultValue: state })}</span>}
      </div>
    </section>
  )
}

export function ReportCard({ item, onNavigate }) {
  const { t } = useTranslation()
  const p = item.payload
  return (
    <section className="ms-card" data-testid="mission-report" aria-label={t('bubbles.mission.reportLabel')}>
      <div className="ms-card-head"><Flag size={12} aria-hidden="true" /> <strong>{t('bubbles.mission.report')}</strong></div>
      <p className="ms-text" style={{ whiteSpace: 'pre-wrap' }}>{p.summary}</p>
      <div className="ms-actions">
        <button type="button" className="btn btn-secondary btn-sm" data-testid="mission-open-report" onClick={() => onNavigate?.('tasks', { sessionId: p.sessionId, report: true })}>
          <ExternalLink size={12} aria-hidden="true" /> {t('bubbles.mission.openReport')}
        </button>
      </div>
    </section>
  )
}

// MissionCards renders the cards of one turn.
export default function MissionCards({ items, conversationId, busy, started, onConfirm, onNavigate }) {
  if (!items?.length) return null
  return (
    <div className="ms-cards" data-testid="mission-cards">
      {items.map(it => it.type === 'propose' ? <ProposalCard key={it.id} item={it} conversationId={conversationId} busy={busy} started={started} onConfirm={(p) => onConfirm(it.id, p)} />
        : it.type === 'plan' ? <PlanCard key={it.id} item={it} onNavigate={onNavigate} />
          : <ReportCard key={it.id} item={it} onNavigate={onNavigate} />)}
    </div>
  )
}
