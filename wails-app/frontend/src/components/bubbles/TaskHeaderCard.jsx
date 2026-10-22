import { useEffect, useState } from 'react'
import { aitaskApi, taskIsDone } from '../SendToCoderApi.js'

const POLL_MS = 4000

// TaskHeaderCard sits at the top of a coder bubble that is an AI task (#957):
// what was asked, its status (kept fresh while it runs), a way back to the tab
// it came from, and Cancel. task = { taskId, goal, status, origin: { page, label } }.
export function TaskHeaderCard({ task, onNavigate }) {
  const [status, setStatus] = useState(task.status || '')
  const [error, setError] = useState('')
  const [cancelling, setCancelling] = useState(false)

  useEffect(() => {
    if (!task.taskId || taskIsDone(status)) return undefined
    let live = true
    const id = setInterval(() => {
      aitaskApi.get(task.taskId).then(t => { if (live && t?.status) setStatus(t.status) }).catch(() => {})
    }, POLL_MS)
    return () => { live = false; clearInterval(id) }
  }, [task.taskId, status])

  const cancel = async () => {
    setCancelling(true)
    setError('')
    try {
      const t = await aitaskApi.cancel(task.taskId)
      setStatus(t?.status || 'cancelled')
    } catch (err) {
      setError(String(err?.message || err))
    } finally {
      setCancelling(false)
    }
  }

  const origin = task.origin
  return (
    <section aria-label="Coder task" data-testid="task-header" style={{
      display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', padding: '6px 12px',
      borderBottom: '1px solid var(--border)', fontFamily: 'var(--font-mono)', fontSize: 11, flexShrink: 0,
    }}>
      <span style={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'var(--text)' }} title={task.goal}>{task.goal}</span>
      <span style={{ color: 'var(--cyan)', border: '1px solid var(--border-bright)', borderRadius: 999, padding: '1px 7px' }}>{status || 'queued'}</span>
      {origin?.page && onNavigate && (
        <button type="button" className="btn btn-ghost btn-sm" onClick={() => onNavigate(origin.page)}>Open in {origin.label || origin.page}</button>
      )}
      {!taskIsDone(status) && (
        <button type="button" className="btn btn-ghost btn-sm" onClick={cancel} disabled={cancelling}>Cancel</button>
      )}
      {error && <span role="alert" style={{ color: 'var(--red)', width: '100%' }}>{error}</span>}
    </section>
  )
}
