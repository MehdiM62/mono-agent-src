import { useTranslation } from 'react-i18next'
import { statusMeta } from '../lib/commsStatus.js'

const linkStyle = { background: 'none', border: 'none', padding: 0, color: 'var(--cyan)', cursor: 'pointer', font: 'inherit', textDecoration: 'underline' }

// StatusBadge: text + glyph + colour for a communication's approval state.
export function StatusBadge({ status }) {
  const { t } = useTranslation()
  if (!status) return null
  const { color, glyph } = statusMeta(status)
  return (
    <span data-testid="comms-status" data-status={status} style={{
      display: 'inline-flex', alignItems: 'center', gap: 4, flexShrink: 0,
      padding: '1px 6px', borderRadius: 4, fontSize: 9, fontFamily: 'var(--font-mono)',
      color, border: `1px solid ${color}`, background: 'transparent',
    }}>
      <span aria-hidden="true">{glyph}</span>{t(`comms.status.${status}`, { defaultValue: status })}
    </span>
  )
}

// CommsLinks: "Recipient" (a link to the person in People) and "See in
// Communications" for an item that carries a person and/or a HIL ref.
export function CommsLinks({ personId, hilRef, name, onProfile, onNavigate }) {
  const { t } = useTranslation()
  const showPerson = !!personId && !!onProfile
  const showComms = !!(hilRef || personId) && !!onNavigate
  if (!showPerson && !showComms) return null
  return (
    <div data-testid="comms-links" style={{ display: 'flex', gap: 10, alignItems: 'center', fontSize: 11, fontFamily: 'var(--font-mono)', color: 'var(--text-muted)' }}>
      {showPerson && (
        <span>{t('comms.recipient')}: <button type="button" style={linkStyle} onClick={() => onProfile(personId)}>{name || t('comms.openPerson')}</button></span>
      )}
      {showComms && (
        <button type="button" style={linkStyle} onClick={() => onNavigate('communications', { hil_ref: hilRef, person: hilRef ? '' : personId })}>{t('comms.seeInComms')}</button>
      )}
    </div>
  )
}
