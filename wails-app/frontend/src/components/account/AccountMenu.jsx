// The signed-in monoes.me account, in the sidebar's footer: who it is and a way
// to sign out. Signing out asks the gate again, and a machine with no sign-in
// is then locked to the sign-in screen. Keys are written out in full for the
// key scan.
import { useContext, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { User } from 'lucide-react'
import LogInToMonoesButton from '../library/LogInToMonoesButton.jsx'
import { AccountContext } from '../../lib/accountContext.js'
import { useAccountView } from '../../lib/accountGate.js'
import { account, onAccountLogin } from '../../services/account.js'

const mono = { fontFamily: 'var(--font-mono)', fontSize: 10.5 }

export default function AccountMenu() {
  const gate = useContext(AccountContext)
  return gate ? <Menu gate={gate} /> : null
}

function Menu({ gate }) {
  const { t } = useTranslation()
  const { status } = useAccountView(gate)
  const [open, setOpen] = useState(false)

  const user = status?.user
  const label = user ? (user.username ? `@${user.username}` : (user.name || user.email)) : t('account.menu.notSignedIn')
  const signedIn = { logged_in: !!user, user: user ?? null }
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginBottom: 8 }}>
      <button
        className="btn btn-ghost btn-sm"
        aria-expanded={open}
        aria-label={t('account.menu.label')}
        title={user?.email || ''}
        onClick={() => setOpen(o => !o)}
        style={{ justifyContent: 'flex-start', gap: 8, minWidth: 0 }}
      >
        <User size={13} aria-hidden="true" style={{ flexShrink: 0 }} />
        <span style={{ ...mono, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{label}</span>
      </button>
      {open && (
        <div style={{ ...mono, display: 'flex', flexDirection: 'column', gap: 6, padding: '4px 6px', color: 'var(--text-secondary)' }}>
          {user?.email && <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{user.email}</span>}
          {user && status?.plan && <span style={{ color: 'var(--text-muted)' }}>{t('account.menu.plan', { plan: status.plan })}</span>}
          <LogInToMonoesButton service={account} onLogin={onAccountLogin} status={signedIn} onStatusChange={() => gate.check('manual')} />
        </div>
      )}
    </div>
  )
}
