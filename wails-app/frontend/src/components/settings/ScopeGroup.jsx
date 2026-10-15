import { useTranslation } from 'react-i18next'

// The two scopes of the Settings page. "global" settings belong to this machine
// and are the same whichever profile is active; "profile" settings belong to
// the active profile only.

const mono = 'var(--font-mono)'

// ScopeGroup heads one scope: a title and one plain line saying what the
// settings under it apply to. `profileName` is the active profile's name, when
// it is known.
export function ScopeGroup({ scope, profileName, children }) {
  const { t } = useTranslation()
  const isProfile = scope === 'profile'
  const line = isProfile
    ? (profileName ? t('settings.scope.profileLine', { name: profileName }) : t('settings.scope.profileLineNoName'))
    : t('settings.scope.globalLine')
  return (
    <section data-testid={`scope-${scope}`} aria-labelledby={`scope-${scope}-title`} style={{ marginBottom: 36 }}>
      <div style={{ borderBottom: '1px solid var(--border-bright)', paddingBottom: 10, marginBottom: 20 }}>
        <h2 id={`scope-${scope}-title`} style={{ margin: 0, fontFamily: mono, fontSize: 14, fontWeight: 700, color: 'var(--text)', letterSpacing: 0.5 }}>
          {isProfile ? t('settings.scope.profileTitle') : t('settings.scope.globalTitle')}
        </h2>
        <div data-testid={`scope-${scope}-line`} style={{ fontFamily: 'var(--font-body)', fontSize: 11.5, color: 'var(--text-muted)', marginTop: 4, lineHeight: 1.5 }}>
          {line}
        </div>
      </div>
      {children}
    </section>
  )
}
