// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/react'
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (k) => k }) }))
vi.mock('../../components/account/AccountBanners.jsx', () => ({ SignInDialog: () => <div role="dialog">sign in</div> }))
import GetStartedCard from './GetStartedCard.jsx'
import { getStartedSteps } from './getStarted.js'

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
