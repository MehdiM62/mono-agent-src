import { useState, useEffect, useCallback } from 'react'
import { ShieldAlert, X } from 'lucide-react'
import { api } from '../../services/api.js'

// Agent browser permissions (plan C8): what AI agents may do in the
// browsers that have the MonoAgent Bridge extension, as a level per agent
// kind (coder chats, org roles, chat and workflow runs, other agents) plus
// site rules that allow or block a site for all of them. Machine-wide, not a
// profile's. A person using a terminal is never restricted. Everything goes
// through `monoagentcli browser permissions` (app_browser_permissions.go);
// the levels, their labels and descriptions are the CLI's catalog.

const mono = 'var(--font-mono)'
const card = {
  background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 'var(--radius-lg)',
  padding: '16px 20px', display: 'flex', flexDirection: 'column', gap: 12, marginBottom: 16,
}
const label = { fontFamily: mono, fontSize: 10, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 1 }
const hint = { fontFamily: 'var(--font-body)', fontSize: 10.5, color: 'var(--text-muted)', lineHeight: 1.5 }
const errText = { fontFamily: mono, fontSize: 10.5, color: 'var(--red)', lineHeight: 1.5, wordBreak: 'break-word' }
const riskText = { display: 'flex', alignItems: 'flex-start', gap: 6, fontFamily: 'var(--font-body)', fontSize: 10.5, color: 'var(--red)', lineHeight: 1.5, marginTop: 4 }
const row = { display: 'flex', alignItems: 'center', gap: 12, borderTop: '1px solid var(--border)', paddingTop: 10 }
const name = { fontFamily: 'var(--font-body)', fontSize: 12.5, color: 'var(--text)' }
const tag = (color) => ({ fontFamily: mono, fontSize: 9, color, border: `1px solid ${color}`, borderRadius: 4, padding: '0 5px', marginLeft: 8, letterSpacing: 0.5, opacity: 0.85 })

// The riskiest level: any site, and form submits go through unasked.
export const RISKIEST_LEVEL = 'full-no-approvals'

const errMsg = (e) => String(e?.message || e || 'unknown error')

export default function BrowserPermissionsSection() {
  const [perm, setPerm] = useState(null)
  const [loadErr, setLoadErr] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState('')
  const [origin, setOrigin] = useState('')
  const [rule, setRule] = useState('allow')

  const load = useCallback(async () => {
    try { setPerm(await api.browserPermissions()); setLoadErr('') } catch (e) { setLoadErr(errMsg(e)) }
  }, [])
  useEffect(() => { load() }, [load])

  // run makes one change through the CLI, then reads the settings back.
  const run = async (what, fn) => {
    setBusy(what); setErr('')
    try { await fn(); await load(); return true } catch (e) { setErr(errMsg(e)); return false } finally { setBusy('') }
  }

  const addRule = async (e) => {
    e.preventDefault()
    const o = origin.trim()
    if (!o) return
    if (await run('site', () => api.setBrowserSiteRule(o, rule))) setOrigin('')
  }

  if (!perm) {
    return (
      <div style={card} data-testid="browser-permissions">
        {loadErr ? <div style={errText} role="alert">{loadErr}</div> : <div style={hint}>Loading…</div>}
      </div>
    )
  }
  const catalog = Array.isArray(perm.catalog) ? perm.catalog : []
  const kinds = Array.isArray(perm.kinds) ? perm.kinds : []
  const sites = Array.isArray(perm.sites) ? perm.sites : []
  const levelOf = (id) => catalog.find(c => c.id === id)

  return (
    <div style={card} data-testid="browser-permissions">
      <div style={label}>Agent browser permissions</div>
      <div style={hint}>
        What AI agents may do in your browsers through the MonoAgent Bridge, for this whole computer. Local and dev
        pages (localhost, *.test, …) are always allowed unless a kind is Off. You, in a terminal, are never restricted.
      </div>
      <div style={hint} data-testid="kind-token-note">
        MonoAgent tells coder chats and org roles apart by a token it gives the agents it starts. An agent that reads
        another process&apos;s environment could borrow that kind.
      </div>
      {err && <div style={errText} role="alert">{err}</div>}

      {kinds.map(k => {
        const level = levelOf(k.level)
        return (
          <div key={k.kind} data-kind={k.kind} style={{ ...row, alignItems: 'flex-start' }}>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={name}>
                {k.label}
                {k.isDefault && <span style={tag('var(--text-muted)')}>default</span>}
              </div>
              <div style={hint}>{level ? level.description : k.level}</div>
              {k.level === RISKIEST_LEVEL && (
                <div style={riskText} data-testid="permission-risk">
                  <ShieldAlert size={12} style={{ flexShrink: 0, marginTop: 1 }} />
                  <span>The riskiest level: these agents can act, submit forms and run scripts on any site, signed in as you, without asking. A script can read that site&apos;s cookies and storage, and a page can steer them.</span>
                </div>
              )}
            </div>
            <select
              aria-label={`Browser permission for ${k.label}`}
              value={k.level}
              disabled={!!busy}
              onChange={e => run(`kind:${k.kind}`, () => api.setBrowserPermission(k.kind, e.target.value))}
              style={{ minWidth: 230 }}
            >
              {catalog.map(c => (
                <option key={c.id} value={c.id} title={c.description}>{c.id === RISKIEST_LEVEL ? '⚠ ' : ''}{c.label}</option>
              ))}
              {!level && <option value={k.level}>{k.level}</option>}
            </select>
          </div>
        )
      })}

      <div style={{ ...label, marginTop: 6 }}>Site rules</div>
      <div style={hint} data-testid="site-rules-note">
        Block keeps every agent off a site and all its subdomains, on any scheme and port, whatever its level. Allow
        is exact (that scheme, host and port; write *.host for its subdomains) and opens the site to agents at{' '}
        {levelOf('dev')?.label || 'dev'} or {levelOf('ask')?.label || 'ask'}.
      </div>
      {sites.length === 0 && <div style={hint}>No site rules yet.</div>}
      {sites.map(s => (
        <div key={s.origin} data-origin={s.origin} style={row}>
          <span style={{ ...name, fontFamily: mono, fontSize: 11.5, flex: 1, minWidth: 0, wordBreak: 'break-all' }}>{s.origin}</span>
          <span style={tag(s.rule === 'block' ? 'var(--red)' : 'var(--green-neon)')}>{s.rule === 'block' ? 'Block' : 'Allow'}</span>
          <button type="button" className="btn btn-ghost btn-sm" disabled={!!busy}
            aria-label={`Remove the rule for ${s.origin}`} title="Remove this rule"
            onClick={() => run('site', () => api.setBrowserSiteRule(s.origin, 'clear'))}>
            <X size={12} />
          </button>
        </div>
      ))}
      <form onSubmit={addRule} style={{ ...row, flexWrap: 'wrap' }}>
        <input className="form-input" aria-label="Site" placeholder="https://example.com" spellCheck={false}
          value={origin} onChange={e => setOrigin(e.target.value)} disabled={!!busy}
          style={{ flex: 1, minWidth: 180, fontFamily: mono, fontSize: 12 }} />
        <select aria-label="Rule for the site" value={rule} onChange={e => setRule(e.target.value)} disabled={!!busy} style={{ minWidth: 90 }}>
          <option value="allow">Allow</option>
          <option value="block">Block</option>
        </select>
        <button type="submit" className="btn btn-primary btn-sm" disabled={!!busy || !origin.trim()}>Add rule</button>
      </form>
    </div>
  )
}
