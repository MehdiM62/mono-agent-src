import { useState } from 'react'

const chip = (tone) => ({ color: tone, border: `1px solid ${tone}`, borderRadius: 999, padding: '1px 7px' })
const line = { width: '100%', display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', minWidth: 0 }

// The result is data from an agent: shown as plain text only, every field
// capped, never as HTML, markdown or a link.
const text = (v, max) => {
  const s = typeof v === 'string' ? v : v == null ? '' : String(v)
  return s.length > max ? `${s.slice(0, max - 1)}…` : s
}
const count = (v) => (Number.isFinite(v) ? Math.max(0, Math.floor(v)) : 0)

function Chip({ tone, children }) {
  return <span style={chip(tone)}>{children}</span>
}

function Changes({ changes }) {
  const [open, setOpen] = useState(false)
  const items = (Array.isArray(changes) ? changes : []).slice(0, 50)
  if (items.length === 0) return null
  return (
    <span style={{ width: '100%' }}>
      <button type="button" className="btn btn-ghost btn-sm" aria-expanded={open} onClick={() => setOpen(o => !o)}>
        {open ? 'Hide' : 'Show'} {items.length} change{items.length === 1 ? '' : 's'}
      </button>
      {open && (
        <ul data-testid="fix-changes" style={{ margin: '4px 0 0', paddingLeft: 18, color: 'var(--text-muted)' }}>
          {items.map((c, i) => <li key={i} style={{ overflowWrap: 'anywhere' }}>{text(typeof c === 'string' ? c : JSON.stringify(c), 300)}</li>)}
        </ul>
      )}
    </span>
  )
}

// TaskResultLine is the kind-specific line under a completed task's header
// (#958). Unknown kinds render nothing.
export function TaskResultLine({ kind, result, onNavigate }) {
  if (!result || typeof result !== 'object') return null
  if (kind === 'workflow-generate') {
    const id = typeof result.workflowId === 'string' ? result.workflowId : ''
    return (
      <div data-testid="task-result" style={line}>
        <span style={{ color: 'var(--text)', overflowWrap: 'anywhere' }}>Created {text(result.name, 120) || 'workflow'} ({count(result.nodeCount)} nodes)</span>
        {result.validation?.ok === false && (
          <span title={(Array.isArray(result.validation.errors) ? result.validation.errors : []).slice(0, 5).map(e => text(e, 200)).join('\n')}>
            <Chip tone="var(--yellow, #fbbf24)">needs fixes</Chip>
          </span>
        )}
        {id && onNavigate && <button type="button" className="btn btn-ghost btn-sm" onClick={() => onNavigate('noderunner', { workflowId: id })}>Open workflow</button>}
        {result.summary && <span style={{ width: '100%', color: 'var(--text-muted)', overflowWrap: 'anywhere' }}>{text(result.summary, 400)}</span>}
      </div>
    )
  }
  if (kind === 'recording-analyze') {
    return (
      <div data-testid="task-result" style={line}>
        <span style={{ color: 'var(--text)', overflowWrap: 'anywhere' }}>{text(result.summary, 400)}</span>
        {result.saved && <Chip tone="var(--green-neon, #10b981)">saved</Chip>}
        {result.verified && <Chip tone="var(--cyan)">verified</Chip>}
      </div>
    )
  }
  if (kind === 'failed-run-fix') {
    return (
      <div data-testid="task-result" style={line}>
        <span style={{ color: 'var(--text)', overflowWrap: 'anywhere' }}>{text(result.diagnosis, 600)}</span>
        {result.needsApproval && <Chip tone="var(--yellow, #fbbf24)">needs approval</Chip>}
        <Changes changes={result.changes} />
      </div>
    )
  }
  return null
}
