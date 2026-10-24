import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { aitaskApplyApi } from '../SendToCoderApi.js'
import { confirm } from '../ConfirmDialog.jsx'
import { Chip, text, isObj, lineStyle, wrapText, mutedText } from './resultText.jsx'

const YELLOW = 'var(--yellow, #fbbf24)'
const VALUE_CAP = 120

// warnKey maps a CLI warning code to the plain wording's key.
function warnKey(code) {
  const c = String(code || '').toLowerCase()
  if (/send|publish|run|post|write/.test(c)) return 'sends'
  if (/cred|secret|token|key/.test(c)) return 'credential'
  if (/expr/.test(c)) return 'expression'
  if (/active/.test(c)) return 'active'
  return 'other'
}

const show = (v) => { try { return typeof v === 'string' ? v : JSON.stringify(v) ?? '' } catch { return '' } }

function Value({ v }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const s = show(v)
  const long = s.length > VALUE_CAP
  return (
    <span style={{ overflowWrap: 'anywhere' }}>
      <code>{open ? text(s, 2000) : text(s, VALUE_CAP)}</code>
      {long && <button type="button" className="btn btn-ghost btn-sm" onClick={() => setOpen(o => !o)}>{open ? t('taskResult.showLess') : t('taskResult.showMore')}</button>}
    </span>
  )
}

function Edits({ edits }) {
  const { t } = useTranslation()
  if (edits.length === 0) return <span style={mutedText}>{t('taskResult.noEdits')}</span>
  return (
    <ul data-testid="fix-diff" style={{ margin: 0, paddingLeft: 18, display: 'flex', flexDirection: 'column', gap: 4, width: '100%' }}>
      {edits.slice(0, 50).map((e, i) => (
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
  const [applyErr, setApplyErr] = useState('')

  useEffect(() => {
    if (!editable || !taskId) return undefined
    let live = true
    aitaskApplyApi.preview(taskId).then(p => { if (live) setPreview(isObj(p) ? p : { ok: false, edits: [] }) }).catch(e => { if (live) setPreviewErr(String(e?.message || e)) })
    return () => { live = false }
  }, [editable, taskId])

  const edits = Array.isArray(preview?.edits) ? preview.edits.filter(isObj) : []
  const warnings = Array.isArray(preview?.warnings) ? preview.warnings.filter(isObj).slice(0, 10) : []
  const workflowId = text(preview?.workflowId || result.workflowId, 200)
  const canApply = !!preview && preview.ok !== false && !preview.error && edits.length > 0 && !applied

  const apply = async () => {
    const nodes = new Set(edits.map(e => e.node)).size
    const ok = await confirm(t('taskResult.applyBody', { fields: edits.length, nodes, name: text(preview.workflowName || workflowId, 80) }), { title: t('taskResult.applyTitle'), confirmLabel: t('taskResult.applyConfirm') })
    if (!ok) return
    setBusy(true); setApplyErr('')
    try {
      const r = await aitaskApplyApi.apply(taskId)
      setApplied({ count: Number.isFinite(r?.applied) ? r.applied : edits.length, workflowId: text(r?.workflowId || workflowId, 200) })
    } catch (e) { setApplyErr(String(e?.message || e)) } finally { setBusy(false) }
  }

  return (
    <div data-testid="task-result" style={{ ...lineStyle, alignItems: 'flex-start' }}>
      <span style={wrapText}>{text(result.diagnosis, 600)}</span>
      {fix.description && <span style={{ ...mutedText, width: '100%' }}>{text(fix.description, 400)}</span>}
      {fix.kind === 'advice' && <span style={mutedText}>{t('taskResult.adviceOnly')}</span>}
      {editable && !preview && !previewErr && <span style={mutedText}>{t('taskResult.previewLoading')}</span>}
      {previewErr && <span role="alert" style={{ color: 'var(--red)', width: '100%' }}>{t('taskResult.previewError', { error: text(previewErr, 300) })}</span>}
      {preview && <Edits edits={edits} />}
      {preview && preview.ok === false && <span role="alert" style={{ color: 'var(--red)', width: '100%' }}>{text(preview.error, 300) || t('taskResult.previewInvalid')}</span>}
      {warnings.map((w, i) => <Chip key={i} tone={YELLOW} title={text(w.text, 200)}>{t(`taskResult.warn.${warnKey(w.code)}`)}</Chip>)}
      {editable && preview && !applied && (
        <button type="button" className="btn btn-primary btn-sm" onClick={apply} disabled={!canApply || busy}>{busy ? t('taskResult.applying') : t('taskResult.apply')}</button>
      )}
      {applied && (
        <span role="status" style={{ ...wrapText, width: '100%' }}>
          {t('taskResult.applied', { count: applied.count })}
          {applied.workflowId && onNavigate && <> <button type="button" className="btn btn-ghost btn-sm" onClick={() => onNavigate('noderunner', { workflowId: applied.workflowId })}>{t('taskResult.openWorkflow')}</button></>}
        </span>
      )}
      {applyErr && <span role="alert" style={{ color: 'var(--red)', width: '100%' }}>{t('taskResult.applyError', { error: text(applyErr, 300) })}</span>}
    </div>
  )
}
