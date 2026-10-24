import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { confirm } from '../ConfirmDialog.jsx'
import { aitaskBudgetApi, money } from '../settings/aitaskBudgetApi.js'
import { PLAIN_DECIMAL } from '../settings/AiTaskBudgetsSection.jsx'

const mono = 'var(--font-mono)'
const note = { flexBasis: '100%', fontFamily: mono, fontSize: 10, color: 'var(--text-muted)' }

// checkLower is the client-side check of a new cap against the current cap and
// what is already spent: a plain decimal, strictly below the cap, above the
// spend. Returns an i18n key (with params) or null. The CLI stays the
// authority; this only spares a round trip.
export function checkLower(text, cap, spent) {
  if (!PLAIN_DECIMAL.test(text.trim()) || text.trim() !== text) return { key: 'bubbles.pane.lowerNotNumber' }
  const usd = Number(text)
  if (!Number.isFinite(usd)) return { key: 'bubbles.pane.lowerNotNumber' }
  if (!(usd < cap)) return { key: 'bubbles.pane.lowerNotBelow', params: { cap: money(cap) } }
  if (spent != null && !(usd > spent)) return { key: 'bubbles.pane.lowerBelowSpent', params: { spent: money(spent) } }
  return null
}

// CapLower lowers the cap of the chat from the pane: the task's, or, for a
// chat with no task, its own conversation cap under the global one. It never
// raises, and is only offered where some cap applies.
export function CapLower({ taskId, conversationId, cost, enabled, confined, onLowered }) {
  const { t } = useTranslation()
  const [text, setText] = useState('')
  const [error, setError] = useState('')
  const [done, setDone] = useState('')
  const [busy, setBusy] = useState(false)

  if (cost === undefined) return null
  if (confined) return <span style={note} data-testid="pane-lower-note">{t('bubbles.pane.lowerConfined')}</span>
  if (!cost || !conversationId) return <span style={note} data-testid="pane-lower-note">{t('bubbles.pane.lowerNoOwnCap')}</span>
  // The global spend is not this chat's, so it is not compared with the new cap.
  const byTask = !!taskId && cost.scope === 'task'
  const spent = byTask ? cost.spentUsd : null

  const submit = async (e) => {
    e.preventDefault()
    if (busy || !enabled) return
    setDone('')
    const bad = checkLower(text, cost.capUsd, spent)
    if (bad) { setError(t(bad.key, bad.params)); return }
    setError('')
    const usd = Number(text)
    const ok = await confirm(t('bubbles.pane.lowerConfirmBody'), {
      title: t('bubbles.pane.lowerConfirmTitle', { usd: money(usd) }), confirmLabel: t('bubbles.pane.lowerApply'), cancelLabel: t('bubbles.pane.cancel'),
    })
    if (!ok) return
    setBusy(true)
    try {
      await (byTask ? aitaskBudgetApi.lower('task', taskId, usd) : aitaskBudgetApi.lower('conversation', conversationId, usd))
      setText('')
      setDone(t('bubbles.pane.lowerDone', { usd: money(usd) }))
      onLowered()
    } catch (err) {
      setError(t('bubbles.pane.lowerRefused', { message: String(err?.message || err) }))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap', flexBasis: '100%' }} data-testid="pane-lower">
      <input className="form-input" inputMode="decimal" placeholder="USD" value={text} disabled={!enabled || busy}
        aria-label={t('bubbles.pane.lowerLabel')} onChange={e => { setText(e.target.value); setError(''); setDone('') }} style={{ width: 90, fontFamily: mono, fontSize: 11 }} />
      <button type="submit" className="btn btn-ghost btn-sm" style={{ fontSize: 10, padding: '1px 6px' }} disabled={!enabled || busy || text === ''}>{t('bubbles.pane.lowerCap')}</button>
      {!enabled && <span style={note}>{t('bubbles.pane.lowerNeedsAccess')}</span>}
      <div role="status" aria-live="polite" style={note}>{done}</div>
      {error && <div role="alert" style={{ ...note, color: 'var(--red)' }}>{error}</div>}
    </form>
  )
}
