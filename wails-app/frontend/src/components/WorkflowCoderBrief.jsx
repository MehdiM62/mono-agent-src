import { useState } from 'react'
import SendToCoderButton from './SendToCoderButton.jsx'

// "Build with coder": describe the workflow, then hand the brief to the coder
// as a workflow-generate task. The coder returns a validated draft for the user
// to review and save; nothing is saved or run for them. Building a workflow by
// hand, from a template or by import is unchanged.
export default function WorkflowCoderBrief({ origin }) {
  const [brief, setBrief] = useState('')
  const goal = brief.trim()
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 6, width: 320, maxWidth: '100%' }}>
      <label htmlFor="wf-coder-brief" style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-muted)' }}>
        Or describe what it should do and let the coder build a draft
      </label>
      <textarea id="wf-coder-brief" rows={3} value={brief} onChange={e => setBrief(e.target.value)}
        placeholder="Every morning, fetch the top stories and save them to a table"
        style={{ fontFamily: 'var(--font-mono)', fontSize: 12, resize: 'vertical' }} />
      {goal
        ? <SendToCoderButton kind="workflow-generate" label="Build with coder" goal={goal} origin={origin} />
        : <button type="button" className="btn btn-secondary btn-sm" disabled>Build with coder</button>}
    </div>
  )
}
