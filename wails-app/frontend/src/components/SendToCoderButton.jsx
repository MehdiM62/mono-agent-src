import { useState } from 'react'
import { Bot } from 'lucide-react'
import { useSendToCoder } from './bubbles/CoderBubbleContext.jsx'
import { aitaskApi } from './SendToCoderApi.js'

const noteStyle = { fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-muted)', lineHeight: 1.5 }

// SendToCoderButton hands a goal to the coder as an AI task (#957) and opens
// the task's chat as a bubble. It is the opt-in beside an AI action that
// otherwise runs as a one-shot: with coder mode off it says so and links to
// Settings, never turning coder on itself, and the one-shot stays available.
// context is the CLASS=PATH entries or selection-text=<text> (selectionText()).
// origin = { page, label } names the tab the task came from.
export default function SendToCoderButton({ kind = 'generic', goal, context = [], applicationId = '', label = 'Send to coder', origin, className = 'btn btn-secondary btn-sm', style }) {
  const { openDraft, navigate } = useSendToCoder()
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState(null) // { text, error?, settings? }

  const send = async () => {
    if (busy) return
    setBusy(true)
    setNote(null)
    try {
      const state = await aitaskApi.status()
      if (!state?.enabled) {
        setNote({ text: state?.reason || 'Coder mode is off.', settings: true })
        return
      }
      const task = await aitaskApi.submit(kind, typeof goal === 'function' ? goal() : goal, typeof context === 'function' ? context() : context, applicationId)
      if (task?.fallback === 'oneshot') {
        setNote({ text: task.reason || 'Coder mode is off.', settings: true })
        return
      }
      openDraft?.({
        conversationId: task.conversationId,
        task: { taskId: task.taskId, goal: task.goal, status: task.status, origin },
      })
    } catch (err) {
      setNote({ text: String(err?.message || err), error: true })
    } finally {
      setBusy(false)
    }
  }

  return (
    <span style={{ display: 'inline-flex', flexDirection: 'column', gap: 4, alignItems: 'flex-start' }}>
      <button type="button" className={className} style={{ gap: 5, ...style }} onClick={send} disabled={busy} aria-busy={busy}>
        <Bot size={12} aria-hidden="true" /> {busy ? 'Sending…' : label}
      </button>
      {note && (
        <span role={note.error ? 'alert' : 'status'} style={{ ...noteStyle, color: note.error ? 'var(--red)' : noteStyle.color }}>
          {note.text}
          {note.settings && (
            <>
              {' '}This action still runs the regular way.{' '}
              {navigate && <a href="#settings" onClick={e => { e.preventDefault(); navigate('settings') }}>Open Settings</a>}
            </>
          )}
        </span>
      )}
    </span>
  )
}
