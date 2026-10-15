import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../services/api.js'

// What an agent turn does when its runtime has no sandbox for it
// (`monoagentcli sandbox fallback`). The CLI owns the values, the default
// and whether monomind can apply the setting (`capable`); this only renders
// the choice and passes it back.

const mono = 'var(--font-mono)'
const card = {
  background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 'var(--radius-lg)',
  padding: '16px 20px', display: 'flex', flexDirection: 'column', gap: 10, marginBottom: 28,
}
const hint = { fontFamily: 'var(--font-body)', fontSize: 10.5, color: 'var(--text-muted)', lineHeight: 1.5 }

export default function SandboxFallbackSection() {
  const { t } = useTranslation()
  const [info, setInfo] = useState(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try { setInfo(await api.sandboxFallbackGet()); setErr('') } catch (e) { setErr(String(e?.message || e)) }
  }, [])
  useEffect(() => { load() }, [load])

  const choose = async (value) => {
    setBusy(true); setErr('')
    try { setInfo(await api.sandboxFallbackSet(value)) } catch (e) {
      setErr(t('settings.sandboxFallback.saveError', { message: String(e?.message || e) }))
    } finally { setBusy(false) }
  }

  const values = info?.values?.length ? info.values : ['run', 'strictest', 'fail']
  const unavailable = !!info && !info.capable
  return (
    <div data-testid="sandbox-fallback-section" style={card}>
      <label htmlFor="sandbox-fallback-select" style={{ fontFamily: mono, fontSize: 11, color: 'var(--text-secondary)' }}>
        {t('settings.sandboxFallback.label')}
      </label>
      <select
        id="sandbox-fallback-select"
        value={info?.value || 'run'}
        disabled={!info || busy || unavailable}
        onChange={e => choose(e.target.value)}
        style={{ maxWidth: 420 }}
      >
        {values.map(v => <option key={v} value={v}>{t(`settings.sandboxFallback.${v}`)}</option>)}
      </select>
      <div style={hint}>{t('settings.sandboxFallback.hint')}</div>
      {unavailable && <div role="note" style={{ ...hint, color: 'var(--yellow)' }}>{t('settings.sandboxFallback.unavailable')}</div>}
      {info?.source === 'env' && (
        <div role="note" style={{ ...hint, color: 'var(--yellow)' }}>{t('settings.sandboxFallback.envOverride', { env: info.env })}</div>
      )}
      {err && <div role="alert" style={{ ...hint, color: 'var(--red)', wordBreak: 'break-word' }}>{err}</div>}
    </div>
  )
}
