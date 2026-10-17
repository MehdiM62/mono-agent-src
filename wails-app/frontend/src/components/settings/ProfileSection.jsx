import { useEffect, useState } from 'react'
import { Trash2 } from 'lucide-react'
import { UpdateProfile, GetProfileDeleteSummary, DeleteProfile } from '../../wailsjs/go/main/App'
import { iconUrl } from '../orgdesigner/roleIcons.js'
import IconPickerModal from '../orgdesigner/IconPickerModal.jsx'

const card = {
  background: 'var(--surface)',
  border: '1px solid var(--border)',
  borderRadius: 'var(--radius-lg)',
  padding: '16px 20px',
  marginBottom: 16,
}
const mono = { fontFamily: 'var(--font-mono)', fontSize: 11 }

// A table name read as words: "person_messages" -> "person messages".
const words = (table) => table.replace(/_/g, ' ')

/**
 * DeleteProfileDialog — three steps before a profile goes: what is lost,
 * a second "this cannot be undone", then typing the profile's name.
 */
function DeleteProfileDialog({ profile, onClose, onDeleted }) {
  const [step, setStep] = useState(1)
  const [summary, setSummary] = useState(null)
  const tables = summary?.tables ?? null
  const [typed, setTyped] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    GetProfileDeleteSummary(profile.id).then(s => setSummary({ ...s, tables: s?.tables || [] })).catch(e => setError(e?.message || String(e)))
  }, [profile.id])

  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape' && !busy) onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [busy, onClose])

  const total = (tables || []).reduce((n, t) => n + t.rows, 0)

  const remove = async () => {
    setBusy(true)
    setError('')
    try {
      const backup = await DeleteProfile(profile.id)
      onDeleted(backup)
    } catch (e) {
      setError(e?.message || String(e))
      setBusy(false)
    }
  }

  const btn = { padding: '7px 14px', borderRadius: 6, cursor: 'pointer', fontSize: 12, fontFamily: 'var(--font-mono)' }
  return (
    <div className="modal-overlay" onClick={e => { if (e.target === e.currentTarget && !busy) onClose() }}>
      <div role="dialog" aria-modal="true" aria-label="Delete profile" className="modal" style={{ width: 480 }}>
        <div className="modal-title" style={{ color: '#ef4444' }}>Delete profile “{profile.name}” — step {step} of 3</div>

        {step === 1 && (
          <>
            <div style={{ ...mono, color: 'var(--text-secondary)', lineHeight: 1.6, marginBottom: 10 }}>
              Everything stored under this profile will be permanently lost:
            </div>
            {tables === null && !error && <div style={{ ...mono, color: 'var(--text-muted)' }}>Counting…</div>}
            {tables && (
              <div style={{ maxHeight: 200, overflowY: 'auto', border: '1px solid var(--border-dim)', borderRadius: 6, marginBottom: 10 }}>
                {tables.length === 0 && <div style={{ ...mono, padding: '7px 10px', color: 'var(--text-muted)' }}>No stored data.</div>}
                {tables.map(t => (
                  <div key={t.table} style={{ ...mono, display: 'flex', justifyContent: 'space-between', padding: '5px 10px', borderBottom: '1px solid var(--border-dim)' }}>
                    <span>{words(t.table)}</span><span>{t.rows}</span>
                  </div>
                ))}
              </div>
            )}
            <div style={{ ...mono, color: 'var(--text-muted)', lineHeight: 1.6 }}>
              That includes its people and messages, workflows, tasks, publications, vault items and documents, credentials and API keys, chats and orgs.
              The profile’s folder{summary?.folder ? ` (${summary.folder})` : ''} is kept on disk; only the app’s records of the profile are deleted.
            </div>
          </>
        )}

        {step === 2 && (
          <div style={{ ...mono, color: 'var(--text-secondary)', lineHeight: 1.6 }}>
            This cannot be undone. {total > 0 ? `${total} stored item${total === 1 ? '' : 's'} will be gone for good` : 'The profile will be gone for good'},
            and the app has no way to restore it. A copy of the database is saved first, in the backups folder next to it. Running orgs of this profile are stopped first.
            {' This is the profile you are using: the app will switch to another one.'}
          </div>
        )}

        {step === 3 && (
          <>
            <div style={{ ...mono, color: 'var(--text-secondary)', lineHeight: 1.6, marginBottom: 10 }}>
              Final check. Type <strong style={{ color: 'var(--text)' }}>{profile.name}</strong> to delete it.
            </div>
            <input
              className="form-input"
              autoFocus
              value={typed}
              onChange={e => setTyped(e.target.value)}
              placeholder={profile.name}
            />
          </>
        )}

        {error && <div style={{ ...mono, color: 'var(--red)', marginTop: 10 }}>{error}</div>}

        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 10, marginTop: 16 }}>
          <button onClick={onClose} disabled={busy}
            style={{ ...btn, background: 'transparent', border: '1px solid var(--border)', color: 'var(--text-secondary)' }}>
            Cancel
          </button>
          {step < 3 ? (
            <button onClick={() => setStep(step + 1)} disabled={tables === null}
              style={{ ...btn, border: 'none', background: '#ef4444', color: '#fff' }}>
              {step === 1 ? 'I understand, continue' : 'Yes, continue'}
            </button>
          ) : (
            <button onClick={remove} disabled={busy || typed !== profile.name}
              style={{ ...btn, border: 'none', background: '#ef4444', color: '#fff', opacity: typed === profile.name ? 1 : 0.5 }}>
              {busy ? 'Deleting…' : 'Delete forever'}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

/** ProfileSection — rename the profile Settings shows, change its icon, or delete it. */
export default function ProfileSection({ profile }) {
  const [name, setName] = useState(profile?.name || '')
  const [iconPickerOpen, setIconPickerOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [status, setStatus] = useState('')

  useEffect(() => { setName(profile?.name || '') }, [profile?.name])

  if (!profile?.id) return null

  const save = async (nextName, nextIcon) => {
    setStatus('')
    try {
      await UpdateProfile(profile.id, nextName, nextIcon)
      setStatus('Saved')
      // The sidebar's profile switcher reads the list on load.
      window.location.reload()
    } catch (e) {
      setStatus(e?.message || String(e))
    }
  }

  const dirty = name.trim() !== '' && name.trim() !== profile.name
  return (
    <>
      <div style={card}>
        <div style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
          <button
            onClick={() => setIconPickerOpen(true)}
            title="Change icon"
            style={{ padding: 0, border: '2px solid rgba(0,180,216,0.3)', borderRadius: '50%', cursor: 'pointer', background: 'transparent', flexShrink: 0, width: 44, height: 44 }}
          >
            <img src={iconUrl(profile.icon || 'coder')} alt="" style={{ width: '100%', height: '100%', borderRadius: '50%', display: 'block' }} />
          </button>
          <input
            className="form-input"
            value={name}
            onChange={e => setName(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter' && dirty) save(name.trim(), profile.icon) }}
            aria-label="Profile name"
            style={{ flex: 1 }}
          />
          <button className="btn btn-primary btn-sm" disabled={!dirty} onClick={() => save(name.trim(), profile.icon)}>Rename</button>
        </div>
        {status && <div style={{ ...mono, color: 'var(--text-muted)', marginTop: 8 }}>{status}</div>}

        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 16, marginTop: 16, paddingTop: 14, borderTop: '1px solid var(--border)' }}>
          <div style={{ ...mono, color: 'var(--text-muted)', lineHeight: 1.5 }}>
            Deleting a profile removes all its data. You will be asked three times.
          </div>
          <button className="btn btn-secondary btn-sm" onClick={() => setDeleting(true)}
            style={{ display: 'flex', alignItems: 'center', gap: 6, color: '#ef4444', flexShrink: 0 }}>
            <Trash2 size={13} /> Delete profile
          </button>
        </div>
      </div>

      <IconPickerModal
        open={iconPickerOpen}
        currentIconId={profile.icon}
        onSelect={(icon) => save(profile.name, icon)}
        onClose={() => setIconPickerOpen(false)}
      />
      {deleting && (
        <DeleteProfileDialog
          profile={profile}
          onClose={() => setDeleting(false)}
          onDeleted={() => window.location.reload()}
        />
      )}
    </>
  )
}
