import { AlertCircle, X } from 'lucide-react'
import { withoutAgentSetupMarker } from '../lib/agentSetup.js'
import SendToCoderButton from './SendToCoderButton.jsx'
import { selectionText } from './SendToCoderApi.js'

// problemText is the failure as plain text for the coder: the run's error and
// each failed node with its error.
export function problemText(problem) {
  const lines = []
  if (problem.error) lines.push(withoutAgentSetupMarker(problem.error))
  for (const f of problem.failed) lines.push(`${f.name}: ${withoutAgentSetupMarker(f.error || 'failed')}`)
  return lines.join('\n')
}

// failedRunGoal is the task goal for a failed run: which workflow and run, and
// that the coder may fix the workflow but must not run it again on its own.
export function failedRunGoal({ workflowId, workflowName, executionId }) {
  const wf = workflowName ? `workflow "${workflowName}"${workflowId ? ` (${workflowId})` : ''}` : (workflowId ? `workflow ${workflowId}` : 'a workflow')
  const run = executionId ? `run ${executionId}` : 'the last run'
  return `Work out why ${run} of ${wf} failed and fix the workflow. Read the run with the workflow execution commands. Do not run or activate the workflow again unless I ask; if you do, say so.`
}

// The "why did it fail" panel under the editor toolbar: the execution's own
// error and each failed node's. problem is runProblem() from
// lib/runFailure.js; clicking a node row selects that node on the canvas.
// onSendToCoder, when given, adds "Fix with coder": it is called with the
// failure text and returns { goal, origin } (how to frame the task and which
// tab it came from); the failure itself goes to the coder as selection text.
export default function RunProblemPanel({ problem, onSelectNode, onClose, onSendToCoder }) {
  if (!problem) return null
  const failed = problem.status === 'FAILED'
  const tone = failed ? '#ef4444' : '#fbbf24'
  const nodeErrors = problem.failed.filter(f => f.error && f.error !== problem.error)
  const coder = onSendToCoder ? onSendToCoder(problemText(problem)) : null
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
      {coder && (
        <SendToCoderButton kind="failed-run-fix" label="Fix with coder" goal={coder.goal} origin={coder.origin}
          context={() => [selectionText(problemText(problem))]} />
      )}
      <button onClick={onClose} aria-label="Hide error" title="Hide error" style={{
        background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', padding: 2, display: 'flex',
      }}><X size={12} /></button>
    </div>
  )
}
