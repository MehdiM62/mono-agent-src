import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../services/api.js'
import { ACCESS_LEVELS } from '../../lib/chatAccess.js'

const mono = 'var(--font-mono)'
const label = { fontFamily: mono, fontSize: 10, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 1 }
const hint = { fontFamily: 'var(--font-body)', fontSize: 10.5, color: 'var(--text-muted)', lineHeight: 1.5 }

// CoderDefaultAccess is `coder set --default-access`: the level a new chat
// starts at. status is the coder status; onSaved gets the status the CLI
// returns. Disabled, with the reason, while coder mode is off.
export default function CoderDefaultAccess({ status, onSaved }) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [saved, setSaved] = useState(false)
  const enabled = !!status?.enabled
  const current = ACCESS_LEVELS.includes(status?.defaultAccess) ? status.defaultAccess : 'scoped'
  const levelName = (l) => t(`bubbles.pane.level.${l}`)

  const change = async (e) => {
    setBusy(true); setErr(''); setSaved(false)
    try { onSaved(await api.coderSetDefaultAccess(e.target.value)); setSaved(true) } catch (x) { setErr(String(x?.message || x)) } finally { setBusy(false) }
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }} data-testid="coder-default-access">
      <label htmlFor="coder-default-access" style={label}>{t('coderDefault.label')}</label>
      <select id="coder-default-access" value={current} onChange={change} disabled={!enabled || busy}
        style={{ alignSelf: 'flex-start' }}>
        {ACCESS_LEVELS.map(l => <option key={l} value={l}>{levelName(l)}</option>)}
      </select>
      <span style={hint}>{t('coderDefault.hint')}</span>
      {!enabled && <span style={hint}>{t('coderDefault.off')}</span>}
      {enabled && <span data-testid="coder-default-effective" style={hint}>{t('coderDefault.effective', { level: levelName(current) })}</span>}
      {enabled && (current === 'full' || current === 'workspace') && <span data-testid="coder-default-allows" style={{ ...hint, color: 'var(--yellow)' }}>{t(current === 'full' ? 'coderDefault.allowsFull' : 'coderDefault.allowsWorkspace')}</span>}
      {saved && <span role="status" style={{ fontFamily: mono, fontSize: 10.5, color: 'var(--green-neon)' }}>{t('coderDefault.saved')}</span>}
      {err && <span role="alert" style={{ fontFamily: mono, fontSize: 10.5, color: 'var(--red)' }}>{err}</span>}
    </div>
  )
}
