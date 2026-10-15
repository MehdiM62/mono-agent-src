// @vitest-environment jsdom
// Signed out, the whole app is the sign-in screen: the real AccountApp (with the
// real App behind it) renders nothing else, and calls no binding but the ones
// the sign-in screen needs, whatever the page does: a key press, a focus, a
// deep link. wails-app/app_account_gate_test.go holds the Go side's list.
import React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup, act } from '@testing-library/react'

const calls = vi.hoisted(() => [])
const answers = vi.hoisted(() => ({ status: null }))
vi.mock('./wailsjs/go/main/App', async (importOriginal) => {
  const real = await importOriginal()
  const out = {}
  for (const name of Object.keys(real)) {
    out[name] = (...args) => {
      calls.push(name)
      if (name === 'AccountStatus') return answers.status ? Promise.resolve(JSON.stringify(answers.status)) : Promise.reject(new Error('binding missing'))
      return Promise.resolve(JSON.stringify({ error: 'Log in to monoes.me first: monoagentcli account login', login_required: true }))
    }
  }
  return out
})

import i18n from './i18n.js'
import AccountApp from './AccountApp.jsx'
import AccountShell from './components/account/AccountShell.jsx'
import { createAccountGate } from './lib/accountGate.js'
import { account } from './services/account.js'

// What the sign-in screen may call: wails-app/app_account_gate_test.go's openBindings.
const SIGN_IN_SCREEN = new Set(['AccountStatus', 'AccountLogin', 'AccountLoginCancel', 'AccountLoginEmailSend',
  'AccountLoginEmailVerify', 'AccountLogout', 'AppSelfUpdate', 'CheckForUpdate', 'OpenURL'])

const doc = (over = {}) => ({ v: 1, state: 'ok', reason: '', plan: 'free', enforced: true, enforce_from: '2020-01-01T00:00:00Z', ...over })
const LOCKED = doc({ state: 'locked', reason: 'not_logged_in' })

let listeners
beforeEach(async () => {
  await i18n.changeLanguage('en')
  calls.length = 0
  answers.status = LOCKED
  listeners = new Map()
  // The Wails runtime, as far as subscribeEvent needs it.
  window.runtime = {
    EventsOnMultiple: (name, cb) => {
      listeners.set(name, [...(listeners.get(name) || []), cb])
      return () => listeners.set(name, (listeners.get(name) || []).filter(f => f !== cb))
    },
    EventsOff: () => {},
  }
})
afterEach(() => { cleanup(); delete window.runtime; window.location.hash = '' })

const emit = (name, ...data) => (listeners.get(name) || []).forEach(cb => cb(...data))
const outsideSignIn = () => calls.filter(n => !SIGN_IN_SCREEN.has(n))

describe('signed out, the app is the sign-in screen', () => {
  it('renders only the sign-in screen and calls only its bindings', async () => {
    render(<AccountApp />)
    expect(await screen.findByRole('region', { name: 'Sign in to MonoAgent' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Log in to monoes/ })).toBeInTheDocument()
    expect(screen.queryByRole('navigation')).not.toBeInTheDocument()
    expect(document.querySelector('.sidebar-nav, .app-layout, .status-bar')).toBeNull()

    // Keys the app binds, a focus, a deep link, and the events the app listens for.
    for (const key of ['k', ',', 'n', '1', 'Escape']) {
      fireEvent.keyDown(window, { key, ctrlKey: true })
      fireEvent.keyDown(window, { key, metaKey: true })
    }
    window.location.hash = '#/settings'
    fireEvent(window, new HashChangeEvent('hashchange'))
    act(() => { emit('log:entry', { message: 'x' }); emit('tasks:changed', {}); emit('update:available', { update_available: true, latest_version: 'v9' }) })
    await act(async () => { await new Promise(r => setTimeout(r, 300)) })

    expect(screen.getByRole('region', { name: 'Sign in to MonoAgent' })).toBeInTheDocument()
    expect(screen.queryByRole('navigation')).not.toBeInTheDocument()
    expect(calls).toContain('AccountStatus')
    expect(outsideSignIn()).toEqual([])
  })

  it('the email-code variant calls only the sign-in bindings', async () => {
    render(<AccountApp />)
    await screen.findByRole('region', { name: 'Sign in to MonoAgent' })
    fireEvent.click(screen.getByText('Use an email code instead'))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'me@example.test' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send code' }))
    await waitFor(() => expect(calls).toContain('AccountLoginEmailSend'))
    expect(screen.queryByRole('navigation')).not.toBeInTheDocument()
    expect(outsideSignIn()).toEqual([])
  })
})

describe('a page whose Go side cannot be asked', () => {
  it('shows the sign-in screen, not the app, when the status binding fails', async () => {
    answers.status = null
    render(<AccountApp />)
    expect(await screen.findByRole('region', { name: 'MonoAgent could not check your sign-in' })).toBeInTheDocument()
    expect(screen.queryByRole('navigation')).not.toBeInTheDocument()
    expect(outsideSignIn()).toEqual([])
  })
})

describe('the Go side says when the session changed', () => {
  const shell = () => <div data-testid="shell">the app</div>

  it('asks again on account:changed: a sign-out in a terminal brings the sign-in screen up', async () => {
    answers.status = doc()
    render(<AccountShell gate={createAccountGate({ status: account.status })} renderShell={shell} />)
    expect(await screen.findByTestId('shell')).toBeInTheDocument()
    answers.status = LOCKED
    act(() => emit('account:changed'))
    expect(await screen.findByRole('region', { name: 'Sign in to MonoAgent' })).toBeInTheDocument()
    expect(screen.queryByTestId('shell')).not.toBeInTheDocument()
  })
})
