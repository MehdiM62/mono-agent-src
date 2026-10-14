import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { APIKeyList } from '../../../wailsjs/go/main/App'
import ApiKeysBlock from './ApiKeysBlock.jsx'
import { apiError } from './apiError.js'
import { servingListeners } from './apiModel.js'
import { mono } from './ui.jsx'

// The API keys are the active profile's (api_keys.profile_id), unlike the server settings around them, which are
// the machine's (ApiSection part="machine"). This is the keys block on its own, for the profile group of Settings:
// it reads the keys; the status, for the context classes the keys dialog explains, is the API section's own
// (Settings passes it down), so there is one status request and one state.
const card = {
  background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 'var(--radius-lg)',
  padding: '16px 20px', display: 'flex', flexDirection: 'column', gap: 14, marginBottom: 16,
}

export default function ApiKeysSection({ isActive = true, status = null, profileId = '' } = {}) {
  const { t } = useTranslation()
  const tRef = useRef(t)
  tRef.current = t
  const [keys, setKeys] = useState(null)
  const [keysErr, setKeysErr] = useState('')
  const seq = useRef(0)

  const loadKeys = useCallback(async () => {
    const mine = ++seq.current
    try {
      const list = await APIKeyList()
      if (mine === seq.current) { setKeys(list); setKeysErr('') }
    } catch (e) {
      if (mine === seq.current) setKeysErr(apiError(e, tRef.current))
    }
  }, [])

  useEffect(() => {
    loadKeys()
  }, [loadKeys])

  // Coming back to the page: what was read is as old as the visit.
  const wasActive = useRef(isActive)
  useEffect(() => {
    const returned = isActive && !wasActive.current
    wasActive.current = isActive
    if (returned) loadKeys()
  }, [isActive, loadKeys])

  // Another profile has other keys: read them when the profile changes (not for the first one that becomes known).
  const lastProfile = useRef(profileId)
  useEffect(() => {
    const changed = profileId !== lastProfile.current
    const wasKnown = !!lastProfile.current
    lastProfile.current = profileId
    if (changed && wasKnown) loadKeys()
  }, [profileId, loadKeys])

  const contextClasses = [...new Set(servingListeners(status).map(l => l.context_confinement).filter(Boolean))]

  return (
    <div id="settings-api-keys" data-testid="api-keys-section">
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 14 }}>
        <span style={{ fontFamily: mono, fontSize: 10, fontWeight: 700, color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: 2 }}>
          {t('settings.api.keys.title')}
        </span>
        <div style={{ flex: 1, height: 1, background: 'var(--border)' }} />
      </div>
      <div style={card}>
        <ApiKeysBlock keys={keys} err={keysErr} contextClasses={contextClasses} onChanged={loadKeys} onRetry={loadKeys} />
      </div>
    </div>
  )
}
