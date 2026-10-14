// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/react'
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (k) => k }) }))
vi.mock('../../components/account/AccountBanners.jsx', () => ({ SignInDialog: () => <div role="dialog">sign in</div> }))
import GetStartedCard from './GetStartedCard.jsx'
import { getStartedSteps } from './getStarted.js'
import en from '../../locales/en.json'
import es from '../../locales/es.json'

afterEach(cleanup)

describe('GetStartedCard', () => {
  it('shows each step with its state and one action', () => {
    const onNavigate = vi.fn()
    const steps = getStartedSteps({ signedIn: true, okRuns: 0, bridge: 'unpaired', daemonRunning: false })
    render(<GetStartedCard steps={steps} bridge="unpaired" onNavigate={onNavigate} onDismiss={vi.fn()} />)
    expect(screen.getAllByText('dashboard.getStarted.done')).toHaveLength(1) // signed in
    expect(screen.queryByRole('button', { name: 'dashboard.getStarted.signInAction' })).not.toBeInTheDocument()
    expect(screen.getByText('dashboard.getStarted.extensionWhyUnpaired')).toBeInTheDocument()
    expect(screen.getByText('dashboard.getStarted.optional')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'dashboard.getStarted.firstRunAction' }))
    expect(onNavigate).toHaveBeenCalledWith('noderunner', { openTemplates: true })
    fireEvent.click(screen.getByRole('button', { name: 'dashboard.getStarted.extensionAction' }))
    expect(onNavigate).toHaveBeenCalledWith('settings', { section: 'health' })
  })

  // Settings has no pairing action: the unpaired hint names the one-step
  // pairing command instead.
  it('tells an unpaired user the pairing command', () => {
    for (const locale of [en, es]) {
      const text = locale.dashboard.getStarted.extensionWhyUnpaired
      expect(text).toContain('monoagentcli extension pair --open')
      expect(text).not.toMatch(/Settings|Ajustes/)
    }
  })

  it('opens sign-in and dismisses', () => {
    const onDismiss = vi.fn()
    const steps = getStartedSteps({ signedIn: false, okRuns: 0, bridge: 'off', daemonRunning: false })
    render(<GetStartedCard steps={steps} bridge="off" onNavigate={vi.fn()} onDismiss={onDismiss} />)
    fireEvent.click(screen.getByRole('button', { name: 'dashboard.getStarted.signInAction' }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'dashboard.getStarted.dismiss' }))
    expect(onDismiss).toHaveBeenCalled()
  })
})
