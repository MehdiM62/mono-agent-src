import React, { useState } from 'react'
import { Bot } from 'lucide-react'
import { aitaskApi } from '../SendToCoderApi.js'
import { parkedApi } from './parkedApi.js'

const noteStyle = { fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-muted)' }

// ApplyQueueButton hands the selected applications to the coder queue
// (`application fill queue`): it fills them one by one, keeping at most the
// saved "max parked" (default 3) running or waiting for the person's submit.
// The queue runs in the background; each application shows its own state.
// With coder mode off it says why and starts nothing.
export default function ApplyQueueButton({ ids, onStarted, style }) {
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState(null)

  const start = async () => {
    if (busy || ids.length === 0) return
    setBusy(true)
    setNote(null)
    try {
      const state = await aitaskApi.status()
      if (!state?.enabled) {
        setNote({ text: `${state?.reason || 'Coder mode is off.'} Apply works the regular way.` })
        return
      }
      await parkedApi.queue(ids, 0)
      setNote({ text: `Started: the coder fills ${ids.length === 1 ? 'this application' : `these ${ids.length} applications`} one after another and leaves each form for you to check and submit.` })
      onStarted?.()
    } catch (err) {
      setNote({ text: String(err?.message || err), error: true })
    } finally {
      setBusy(false)
    }
  }

  return (
    <span style={{ display: 'inline-flex', flexDirection: 'column', gap: 4, alignItems: 'flex-end' }}>
      <button type="button" style={style} onClick={start} disabled={busy || ids.length === 0} aria-busy={busy}>
        <Bot size={12} aria-hidden="true" /> {busy ? 'Starting…' : `Apply selected with coder${ids.length ? ` (${ids.length})` : ''}`}
      </button>
      {note && <span role={note.error ? 'alert' : 'status'} style={{ ...noteStyle, color: note.error ? '#f43f5e' : noteStyle.color, maxWidth: 360, textAlign: 'right' }}>{note.text}</span>}
    </span>
  )
}
