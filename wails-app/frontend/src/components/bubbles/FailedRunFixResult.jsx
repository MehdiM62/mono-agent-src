import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { aitaskApplyApi } from '../SendToCoderApi.js'
import { confirm } from '../ConfirmDialog.jsx'
import SendToCoderButton from '../SendToCoderButton.jsx'
import { selectionText } from '../SendToCoderApi.js'
import { Chip, text, isObj, lineStyle, wrapText, mutedText } from './resultText.jsx'

const YELLOW = 'var(--yellow, #fbbf24)'
const VALUE_CAP = 120

// WARN_KEYS maps the CLI's exact warning codes to the plain wording's key;
// an unknown code shows the CLI's own text.
const WARN_KEYS = { new_outbound: 'sends', credential_field: 'credential', expression_added: 'expression', workflow_active: 'active' }
const ACTIVE_WARNING = 'workflow_active'
const ACTIVE_REFUSAL = 'workflow_active_needs_confirm'

// errParts splits the binding's "code: message" refusal.
export function errParts(e) {
  const raw = String(e?.message || e || '')
  const m = /^([a-z][a-z0-9_]*): ([\s\S]*)$/.exec(raw)
  return m ? { code: m[1], message: m[2] } : { code: '', message: raw }
}

// The review shows at most this many changed leaves and this many characters
// per value; a preview beyond that cannot be reviewed, so it cannot be applied.
const MAX_LEAVES = 50
const MAX_VALUE = 2000

const show = (v) => { try { return typeof v === 'string' ? v : JSON.stringify(v) ?? '' } catch { return '' } }

function Value({ v }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const s = show(v)
  const long = s.length > VALUE_CAP
  return (
    <span style={{ overflowWrap: 'anywhere' }}>
      <code>{open ? text(s, MAX_VALUE) : text(s, VALUE_CAP)}</code>
      {long && <button type="button" className="btn btn-ghost btn-sm" onClick={() => setOpen(o => !o)}>{open ? t('taskResult.showLess') : t('taskResult.showMore')}</button>}
    </span>
  )
}

function Edits({ edits }) {
  const { t } = useTranslation()
  if (edits.length === 0) return <span style={mutedText}>{t('taskResult.noEdits')}</span>
  return (
    <ul data-testid="fix-diff" style={{ margin: 0, paddingLeft: 18, display: 'flex', flexDirection: 'column', gap: 4, width: '100%' }}>
      {edits.map((e, i) => (
        <li key={i} style={{ overflowWrap: 'anywhere' }}>
          <strong>{text(e.nodeName || e.node, 80)}</strong> · <code>{text(e.path, 120)}</code>
          <div><span style={{ color: 'var(--red)' }}>- <Value v={e.before} /></span></div>
          <div><span style={{ color: 'var(--green-neon, #10b981)' }}>+ <Value v={e.after} /></span></div>
        </li>
      ))}
    </ul>
  )
}

// FailedRunFixResult is a completed failed-run-fix task: the diagnosis, and
// for a workflow-edit fix a readable preview of the proposed edits and an
// Apply button. The agent only proposed; the click applies it.
export default function FailedRunFixResult({ taskId, result, onNavigate }) {
  const { t } = useTranslation()
  const fix = isObj(result.fix) ? result.fix : {}
  const editable = fix.kind === 'workflow-edit'
  const [preview, setPreview] = useState(null) // { ok, edits, warnings, ... }
  const [previewErr, setPreviewErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [applied, setApplied] = useState(null)
  const [applyErr, setApplyErr] = useState(null) // { code, message }
  const [done, setDone] = useState(false) // already applied
  const [reload, setReload] = useState(0) // bumped to ask for a fresh preview

  useEffect(() => {
    if (!editable || !taskId) return undefined
    let live = true
    aitaskApplyApi.preview(taskId).then(p => { if (live) setPreview(isObj(p) ? p : { ok: false, edits: [] }) }).catch(e => { if (live) setPreviewErr(String(e?.message || e)) })
    return () => { live = false }
  }, [editable, taskId, reload])

  // reviewAgain drops what was shown and loads the preview anew.
  const reviewAgain = () => { setPreview(null); setPreviewErr(''); setApplyErr(null); setReload(n => n + 1) }

  const edits = Array.isArray(preview?.edits) ? preview.edits.filter(isObj) : []
  const warnings = Array.isArray(preview?.warnings) ? preview.warnings.filter(isObj).slice(0, 10) : []
  const workflowId = text(preview?.workflowId || result.workflowId, 200)
  const tooBig = edits.length > MAX_LEAVES || edits.some(e => show(e.before).length > MAX_VALUE || show(e.after).length > MAX_VALUE)
  const hash = typeof preview?.definitionHash === 'string' ? preview.definitionHash : ''
  const previewOk = !!preview && preview.ok !== false && !preview.error && edits.length > 0 && !tooBig && hash !== ''
  const canApply = previewOk && !applied && !done && !busy
  const workflowIsActive = warnings.some(w => w.code === ACTIVE_WARNING)

  const confirmActive = () => confirm(t('taskResult.activeBody'), { title: t('taskResult.activeTitle'), confirmLabel: t('taskResult.activeConfirm') })

  // apply runs only from the Apply button's click: busy is set first, so a
  // second click cannot start a second apply while a dialog or the call is open.
  const apply = async () => {
    if (!canApply) return
    const reviewed = hash // the hash of the preview the user is looking at
    setBusy(true); setApplyErr(null)
    try {
      const nodes = new Set(edits.map(e => e.node)).size
      if (!(await confirm(t('taskResult.applyBody', { fields: edits.length, nodes, name: text(preview.workflowName || workflowId, 80) }), { title: t('taskResult.applyTitle'), confirmLabel: t('taskResult.applyConfirm') }))) return
      let allow = false
      if (workflowIsActive) {
        if (!(await confirmActive())) return
        allow = true
      }
      let r
      try {
        r = await aitaskApplyApi.apply(taskId, allow, reviewed)
      } catch (e) {
        const p = errParts(e)
        // The CLI refused an active workflow the preview did not flag: ask, then retry with the flag.
        if (!allow && p.code === ACTIVE_REFUSAL && (await confirmActive())) r = await aitaskApplyApi.apply(taskId, true, reviewed)
        else throw e
      }
      setApplied({ count: Number.isFinite(r?.applied) ? r.applied : edits.length, workflowId: text(r?.workflowId || workflowId, 200) })
    } catch (e) {
      const p = errParts(e)
      if (p.code === 'already_applied') setDone(true)
      else setApplyErr(p)
    } finally { setBusy(false) }
  }

  const errorLine = (() => {
    if (!applyErr) return null
    if (applyErr.code === 'stale_proposal') return t('taskResult.stale')
    if (applyErr.code === 'changed_since_preview') return t('taskResult.changedSincePreview')
    if (applyErr.code === 'too_large') return t('taskResult.tooLarge')
    if (applyErr.code === 'operator_only') return t('taskResult.operatorOnly')
    if (applyErr.code === ACTIVE_REFUSAL) return t('taskResult.activeDeclined')
    return t('taskResult.applyError', { error: text(applyErr.message, 300) })
  })()

  return (
    <div data-testid="task-result" style={{ ...lineStyle, alignItems: 'flex-start' }}>
      <span style={wrapText}>{text(result.diagnosis, 600)}</span>
      {fix.description && <span style={{ ...mutedText, width: '100%' }}>{text(fix.description, 400)}</span>}
      {fix.kind === 'advice' && <span style={mutedText}>{t('taskResult.adviceOnly')}</span>}
      {editable && !preview && !previewErr && <span style={mutedText}>{t('taskResult.previewLoading')}</span>}
      {previewErr && <span role="alert" style={{ color: 'var(--red)', width: '100%' }}>{t('taskResult.previewError', { error: text(previewErr, 300) })}</span>}
      {preview && <Edits edits={edits} />}
      {preview && tooBig && <span role="alert" style={{ color: 'var(--red)', width: '100%' }}>{t('taskResult.tooBigToReview')}</span>}
      {preview && preview.ok === false && <span role="alert" style={{ color: 'var(--red)', width: '100%' }}>{text(preview.error, 300) || t('taskResult.previewInvalid')}</span>}
      {warnings.map((w, i) => <Chip key={i} tone={YELLOW} title={text(w.text, 200)}>{WARN_KEYS[w.code] ? t(`taskResult.warn.${WARN_KEYS[w.code]}`) : text(w.text || w.code, 120)}</Chip>)}
      {editable && !applied && !done && (
        <button type="button" className="btn btn-primary btn-sm" onClick={apply} disabled={!canApply}>{busy ? t('taskResult.applying') : t('taskResult.apply')}</button>
      )}
      {done && <button type="button" className="btn btn-primary btn-sm" disabled>{t('taskResult.alreadyApplied')}</button>}
      {applied && (
        <span role="status" style={{ ...wrapText, width: '100%' }}>
          {t('taskResult.applied', { count: applied.count })}
          {applied.workflowId && onNavigate && <> <button type="button" className="btn btn-ghost btn-sm" onClick={() => onNavigate('noderunner', { workflowId: applied.workflowId })}>{t('taskResult.openWorkflow')}</button></>}
        </span>
      )}
      {errorLine && <span role="alert" style={{ color: 'var(--red)', width: '100%' }}>{errorLine}</span>}
      {applyErr?.code === 'changed_since_preview' && <button type="button" className="btn btn-secondary btn-sm" onClick={reviewAgain}>{t('taskResult.reviewAgain')}</button>}
      {(applyErr?.code === 'stale_proposal' || applyErr?.code === 'too_large') && (
        <SendToCoderButton kind="failed-run-fix" label={t('taskResult.askAgain')} goal="Work out why this workflow run failed and propose a fix."
          context={[selectionText(`workflow: ${text(result.workflowId, 200)}\nexecution: ${text(result.executionId, 200)}`)]}
          origin={{ page: 'noderunner', label: 'Workflows' }} />
      )}
    </div>
  )
}
