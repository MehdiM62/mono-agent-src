// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'
import i18n from '../../i18n.js'
import SandboxFallbackSection from './SandboxFallbackSection.jsx'

const App = {}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  for (const k of ['SandboxFallbackGet', 'SandboxFallbackSet']) App[k] = vi.fn()
  window.go = { main: { App } }
})
afterEach(() => { cleanup(); delete window.go })

const doc = (over = {}) => JSON.stringify({
  setting: 'sandbox.fallback', value: 'run', source: 'default', default: 'run',
  values: ['run', 'strictest', 'fail'], env: 'MONOAGENT_SANDBOX_FALLBACK', capable: true, monomindVersion: '2.24.5', ...over,
})

const label = 'When an agent runtime has no sandbox'

describe('SandboxFallbackSection', () => {
  it('shows the three choices with plain labels, the default selected', async () => {
    App.SandboxFallbackGet.mockResolvedValue(doc())
    render(<SandboxFallbackSection />)
    const select = await screen.findByLabelText(label)
    await waitFor(() => expect(select).not.toBeDisabled())
    expect(select).toHaveValue('run')
    expect([...select.options].map(o => o.textContent)).toEqual([
      'Run without a sandbox (default)', 'Use the strictest sandbox the runtime has', 'Refuse the turn',
    ])
  })

  it('choosing a value saves it through the binding and shows what the CLI answered', async () => {
    App.SandboxFallbackGet.mockResolvedValue(doc())
    App.SandboxFallbackSet.mockResolvedValue(doc({ value: 'strictest', source: 'saved' }))
    render(<SandboxFallbackSection />)
    const select = await screen.findByLabelText(label)
    await waitFor(() => expect(select).not.toBeDisabled())
    fireEvent.change(select, { target: { value: 'strictest' } })
    await waitFor(() => expect(App.SandboxFallbackSet).toHaveBeenCalledWith('strictest'))
    await waitFor(() => expect(select).toHaveValue('strictest'))
  })

  it('is disabled, with an explanation, when monomind lacks the capability', async () => {
    App.SandboxFallbackGet.mockResolvedValue(doc({ capable: false }))
    render(<SandboxFallbackSection />)
    const select = await screen.findByLabelText(label)
    expect(await screen.findByRole('note')).toHaveTextContent("can't apply this setting yet")
    expect(select).toBeDisabled()
  })

  it('says when the environment variable overrides the choice', async () => {
    App.SandboxFallbackGet.mockResolvedValue(doc({ value: 'fail', source: 'env', saved: 'run' }))
    render(<SandboxFallbackSection />)
    expect(await screen.findByRole('note')).toHaveTextContent('MONOAGENT_SANDBOX_FALLBACK')
  })

  it('shows the CLI error when saving fails and keeps the old value', async () => {
    App.SandboxFallbackGet.mockResolvedValue(doc())
    App.SandboxFallbackSet.mockResolvedValue(JSON.stringify({ error: '--set "x": must be one of run', code: 'invalid_input' }))
    render(<SandboxFallbackSection />)
    const select = await screen.findByLabelText(label)
    await waitFor(() => expect(select).not.toBeDisabled())
    fireEvent.change(select, { target: { value: 'fail' } })
    expect(await screen.findByRole('alert')).toHaveTextContent('must be one of run')
    expect(select).toHaveValue('run')
  })

  it('renders Spanish', async () => {
    await i18n.changeLanguage('es')
    App.SandboxFallbackGet.mockResolvedValue(doc())
    render(<SandboxFallbackSection />)
    const select = await screen.findByLabelText('Cuando un entorno de agente no tiene sandbox')
    expect([...select.options].map(o => o.textContent)).toContain('Rechazar el turno')
  })
})
