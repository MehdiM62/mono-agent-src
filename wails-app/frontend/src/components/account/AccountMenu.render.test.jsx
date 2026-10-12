// @vitest-environment jsdom
import React from 'react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'
import AccountMenu from './AccountMenu.jsx'
import { AccountContext } from '../../lib/accountContext.js'

vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (k, o) => (o?.plan ? `${k}:${o.plan}` : k) }) }))
const logout = vi.fn(() => Promise.resolve({ v: 1 }))
vi.mock('../../services/account.js', () => ({
  account: { logout: (...a) => logout(...a), status: vi.fn(), login: vi.fn(), cancelLogin: vi.fn(), sendCode: vi.fn(), verifyCode: vi.fn() },
  onAccountLogin: () => () => {},
}))
vi.mock('../../services/library.js', () => ({ library: {}, onLibraryLogin: () => () => {} }))
vi.mock('../../services/api.js', () => ({ api: { openURL: vi.fn() } }))

afterEach(() => { cleanup(); logout.mockClear() })

const status = { state: 'ok', plan: 'free', user: { id: 'u1', email: 'a@b.c', username: 'Monoes' } }
const gateOf = (check) => ({ check, subscribe: () => () => {}, getSnapshot: () => SNAP })
const SNAP = { phase: 'ready', status, failure: null }

describe('AccountMenu', () => {
  it('renders nothing outside the gate', () => {
    const { container } = render(<AccountMenu />)
    expect(container).toBeEmptyDOMElement()
  })

  it('shows the signed-in account and signs out through the account service, then asks the gate again', async () => {
    const check = vi.fn()
    render(<AccountContext.Provider value={gateOf(check)}><AccountMenu /></AccountContext.Provider>)
    expect(screen.getByText('@Monoes')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'account.menu.label' }))
    expect(screen.getByText('a@b.c')).toBeInTheDocument()
    expect(screen.getByText('account.menu.plan:free')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /library\.logout/ }))
    await waitFor(() => expect(logout).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(check).toHaveBeenCalledWith('manual'))
  })
})
