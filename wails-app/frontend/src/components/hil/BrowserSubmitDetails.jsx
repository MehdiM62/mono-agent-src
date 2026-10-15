import { useEffect, useState } from 'react'
import { api } from '../../services/api.js'
import { ImageLightbox } from '../chat/ChatAttachments.jsx'

// A browser submit approval: a form an agent tried to submit on a page that
// is not local, held until a person approves it (then `monoagentcli browser
// submit --hil <id>` sends it, once). readonly_data is {origin, url, form:
// {action, method, fields: [{name, type, label, value}], submitter},
// screenshot, requested_by}. Sensitive values arrive masked as "•••" and are
// shown as they are.

export const BROWSER_SUBMIT = 'browser.submit'

const mono = 'var(--font-mono)'
const sectionLabel = { fontSize: 10, fontFamily: mono, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 1, marginBottom: 4 }
const box = { padding: '7px 10px', background: 'rgba(255,255,255,0.04)', borderRadius: 6, border: '1px solid rgba(255,255,255,0.07)', fontSize: 12, color: 'var(--text-secondary)', wordBreak: 'break-all' }
const cell = { padding: '5px 8px', borderBottom: '1px solid rgba(255,255,255,0.06)', textAlign: 'left', verticalAlign: 'top', wordBreak: 'break-all' }

export function isBrowserSubmit(item) {
  return item?.node_config?.kind === BROWSER_SUBMIT
}

// requesterParts names who asked: the role (a coder chat's own agent is
// "lead"), the org, the agent and the coder chat, whichever are known.
export function requesterParts(by) {
  if (!by || typeof by !== 'object') return []
  const parts = []
  if (by.role) parts.push(by.role === 'lead' && by.conversationId ? 'coder chat agent' : `role ${by.role}`)
  if (by.org) parts.push(`org ${by.org}`)
  if (by.agentId) parts.push(`agent ${by.agentId}`)
  if (by.conversationId) parts.push(`chat ${String(by.conversationId).slice(0, 8)}`)
  return parts
}

function Section({ label, children }) {
  return (
    <div style={{ marginBottom: 12 }}>
      <div style={sectionLabel}>{label}</div>
      {children}
    </div>
  )
}

// SubmitScreenshot is the page as the agent left it, read through
// ReadHILScreenshot (only from the browser approvals folder).
function SubmitScreenshot({ path }) {
  const [src, setSrc] = useState('')
  const [error, setError] = useState('')
  const [open, setOpen] = useState(false)
  useEffect(() => {
    let live = true
    setSrc('')
    setError('')
    Promise.resolve().then(() => api.readHILScreenshot(path)).then(
      url => { if (live) setSrc(url) },
      err => { if (live) setError(String(err?.message || err)) },
    )
    return () => { live = false }
  }, [path])
  if (error) return <div style={{ ...box, color: 'var(--text-muted)' }}>Screenshot unavailable: {error}</div>
  if (!src) return <div style={{ ...box, color: 'var(--text-muted)' }}>Loading screenshot…</div>
  return (
    <>
      <button type="button" onClick={() => setOpen(true)} aria-label="Open the screenshot" title="Open large" style={{
        padding: 0, border: '1px solid rgba(255,255,255,0.07)', borderRadius: 6, background: 'rgba(255,255,255,0.04)',
        cursor: 'zoom-in', display: 'block', overflow: 'hidden', maxWidth: '100%',
      }}>
        <img src={src} alt="The page when the submit was held" style={{ display: 'block', maxWidth: '100%', maxHeight: 260, objectFit: 'contain' }} />
      </button>
      {open && <ImageLightbox src={src} alt="The page when the submit was held" onClose={() => setOpen(false)} />}
    </>
  )
}

// BrowserSubmitDetails replaces the generic "Context Info" of a browser
// submit item; the card's Approve / Reject stay as they are.
export function BrowserSubmitDetails({ item }) {
  const ro = item.readonly_data || {}
  const form = ro.form || {}
  const fields = Array.isArray(form.fields) ? form.fields : []
  const who = requesterParts(ro.requested_by)
  return (
    <div data-testid="browser-submit-details" style={{ marginBottom: 20 }}>
      <Section label="Submits to">
        <div style={box}>
          <div style={{ color: '#e2e8f0', fontWeight: 600 }}>{ro.origin || 'an unknown origin'}</div>
          {ro.url && <div style={{ fontFamily: mono, fontSize: 11, marginTop: 2 }}>{ro.url}</div>}
          {(form.method || form.action) && (
            <div style={{ fontFamily: mono, fontSize: 11, marginTop: 4, color: 'var(--text-muted)' }}>
              {(form.method || 'GET').toUpperCase()} {form.action || '(this page)'}
            </div>
          )}
        </div>
      </Section>
      <Section label="Requested by">
        <div style={box}>{who.length ? who.join(' · ') : 'an agent (it did not say which)'}</div>
      </Section>
      <Section label={`Form fields (${fields.length})`}>
        {fields.length === 0 ? <div style={{ ...box, color: 'var(--text-muted)' }}>No named fields</div> : (
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12, ...box, padding: 0 }}>
            <thead>
              <tr style={{ fontFamily: mono, fontSize: 10, color: 'var(--text-muted)', textTransform: 'uppercase' }}>
                <th style={cell}>Field</th><th style={cell}>Type</th><th style={cell}>Value</th>
              </tr>
            </thead>
            <tbody>
              {fields.map((f, i) => (
                <tr key={`${f.name}-${i}`}>
                  <td style={cell}>
                    <div style={{ color: '#e2e8f0' }}>{f.label || f.name}</div>
                    {f.label && f.name && f.label !== f.name && <div style={{ fontFamily: mono, fontSize: 10, color: 'var(--text-muted)' }}>{f.name}</div>}
                  </td>
                  <td style={{ ...cell, fontFamily: mono, fontSize: 11, color: 'var(--text-muted)' }}>{f.type}</td>
                  <td style={{ ...cell, fontFamily: mono, fontSize: 11 }}>{f.value}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Section>
      <Section label="Submit control">
        <div style={{ ...box, fontFamily: mono, fontSize: 11 }}>{form.submitter || 'none: the page submitted it by script'}</div>
      </Section>
      <Section label="Screenshot">
        {ro.screenshot ? <SubmitScreenshot path={ro.screenshot} /> : <div style={{ ...box, color: 'var(--text-muted)' }}>No screenshot was saved</div>}
      </Section>
    </div>
  )
}
