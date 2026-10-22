import React, { useEffect, useState } from 'react'
import * as WailsApp from '../../wailsjs/go/main/App'
import SendToCoderButton from '../SendToCoderButton.jsx'
import { sectionCardStyle, subtleBtnStyle } from './constants.jsx'
import { parkedApi } from './parkedApi.js'

const noteStyle = { fontSize: 11, color: 'var(--text-secondary, #94a3b8)', lineHeight: 1.5 }

// ApplicationParkedPanel is what the detail header shows while an application
// waits on the person. awaiting_submit: the coder filled the form and left its
// tab open; the person submits there. When the extension saw a submit in that
// tab it asks, and only Confirm (the same path as "Send", `application send`)
// moves the application to applied. needs_you: why the coder stopped, and a way
// to reopen and refill. Nothing here says an application was submitted: that is
// only true once its status is applied.
export default function ApplicationParkedPanel({ detail, assets, onChanged }) {
  const status = detail?.status
  const id = detail?.id
  const [tab, setTab] = useState(null)
  const [dismissed, setDismissed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    setDismissed(false)
    setTab(null)
    if (status !== 'awaiting_submit' || !id) return undefined
    let live = true
    parkedApi.tab(id).then(t => { if (live) setTab(t) }).catch(() => {})
    return () => { live = false }
  }, [id, status])

  if (status !== 'awaiting_submit' && status !== 'needs_you') return null

  const fill = assets?.fill || {}
  const confirm = async () => {
    setBusy(true)
    setError('')
    try {
      await parkedApi.confirm(id)
      onChanged?.()
    } catch (err) {
      setError(String(err?.message || err))
    } finally {
      setBusy(false)
    }
  }
  const openScreenshot = () => {
    try { WailsApp.OpenPathWithOS(fill.screenshot) } catch (err) { setError(String(err?.message || err)) }
  }

  if (status === 'needs_you') {
    return (
      <section aria-label="Needs you" style={{ ...sectionCardStyle, borderColor: 'rgba(244,63,94,0.35)' }}>
        <strong style={{ fontSize: 12, color: '#f43f5e' }}>Needs you</strong>
        <span style={noteStyle}>{fill.note || 'The coder stopped and needs you to take over.'}</span>
        <div>
          <SendToCoderButton label="Reopen and refill" applicationId={id}
            start={() => parkedApi.resume(id)} doneLabel="filled, waiting for you to submit"
            origin={{ page: 'applications', label: 'Applications' }} />
        </div>
      </section>
    )
  }

  const observed = tab?.submitObservedAt && !dismissed
  return (
    <section aria-label="Waiting for your submit" style={{ ...sectionCardStyle, borderColor: 'rgba(0,180,216,0.35)' }}>
      <strong style={{ fontSize: 12, color: '#00b4d8' }}>Waiting for your submit</strong>
      <span style={noteStyle}>
        The coder filled in the form and stopped. Check it in the open browser tab and submit it there yourself.
        {tab?.tab?.origin ? ` Tab: ${tab.tab.origin}` : ''}
      </span>
      {fill.screenshot && (
        <div>
          <button type="button" style={subtleBtnStyle} onClick={openScreenshot}>View screenshot of the filled form</button>
        </div>
      )}
      {observed && (
        <div role="status" style={{ ...noteStyle, color: 'var(--text-primary, #f1f5f9)', display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
          <span>We saw a submit in that tab. Did you send it?</span>
          <button type="button" style={subtleBtnStyle} onClick={confirm} disabled={busy}>Confirm</button>
          <button type="button" style={subtleBtnStyle} onClick={() => setDismissed(true)} disabled={busy}>Not yet</button>
        </div>
      )}
      {error && <span role="alert" style={{ ...noteStyle, color: '#f43f5e' }}>{error}</span>}
    </section>
  )
}
