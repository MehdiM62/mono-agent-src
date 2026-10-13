import { AlertCircle, X } from 'lucide-react'
import { withoutAgentSetupMarker } from '../lib/agentSetup.js'

// The "why did it fail" panel under the editor toolbar: the execution's own
// error and each failed node's. problem is runProblem() from
// lib/runFailure.js; clicking a node row selects that node on the canvas.
export default function RunProblemPanel({ problem, onSelectNode, onClose }) {
  if (!problem) return null
  const failed = problem.status === 'FAILED'
  const tone = failed ? '#ef4444' : '#fbbf24'
  const nodeErrors = problem.failed.filter(f => f.error && f.error !== problem.error)
  return (
    <div role="alert" data-testid="run-problem" style={{
      display: 'flex', gap: 10, alignItems: 'flex-start',
      padding: '10px 14px', background: failed ? 'rgba(239,68,68,0.08)' : 'rgba(251,191,36,0.08)',
      borderBottom: `1px solid ${failed ? 'rgba(239,68,68,0.3)' : 'rgba(251,191,36,0.3)'}`,
      fontFamily: 'var(--font-mono)', fontSize: 11, color: '#fca5a5', flexShrink: 0,
    }}>
      <AlertCircle size={14} color={tone} style={{ flexShrink: 0, marginTop: 1 }} />
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ color: tone, fontWeight: 700, marginBottom: 3 }}>
          {failed ? 'Run failed' : 'Run finished with errors'}
          {problem.failed.length > 0 && <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>
            {' '}· {problem.failed.length === 1 ? '1 node' : `${problem.failed.length} nodes`} marked on the canvas
          </span>}
        </div>
        {problem.error && (
          <div style={{ color: '#fecaca', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{withoutAgentSetupMarker(problem.error)}</div>
        )}
        {nodeErrors.map(f => (
          <button key={f.id} onClick={() => onSelectNode?.(f.id)} style={{
            display: 'block', width: '100%', textAlign: 'left', marginTop: 4, padding: 0,
            background: 'none', border: 'none', cursor: 'pointer',
            fontFamily: 'var(--font-mono)', fontSize: 11, color: '#fecaca', whiteSpace: 'pre-wrap', wordBreak: 'break-word',
          }}>
            <span style={{ color: tone, textDecoration: 'underline' }}>{f.name}</span>: {withoutAgentSetupMarker(f.error)}
          </button>
        ))}
      </div>
      <button onClick={onClose} aria-label="Hide error" title="Hide error" style={{
        background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', padding: 2, display: 'flex',
      }}><X size={12} /></button>
    </div>
  )
}
