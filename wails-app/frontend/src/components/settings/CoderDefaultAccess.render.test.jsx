// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'
import '../../i18n.js'
import CoderModeSection from './CoderModeSection.jsx'

const App = {}
beforeEach(() => {
  for (const k of ['CoderStatus', 'CoderSetDefaultAccess']) App[k] = vi.fn()
  window.go = { main: { App } }
})
afterEach(() => { cleanup(); delete window.go })

const status = (over = {}) => JSON.stringify({ enabled: true, ready: true, workspaceRoot: '/w', runtime: 'claude', defaultAccess: 'full', ...over })

describe('Default access for new chats', () => {
  it('shows the effective level with the one-sentence warning for full', async () => {
    App.CoderStatus.mockResolvedValue(status())
    render(<CoderModeSection />)
    const sel = await screen.findByLabelText('Default access for new chats')
    expect(sel).toHaveValue('full')
    expect([...sel.querySelectorAll('option')].map(o => o.value)).toEqual(['scoped', 'coding', 'workspace', 'full'])
    expect(screen.getByTestId('coder-default-allows')).toHaveTextContent('Runs commands and changes files in the folder you pick, without asking.')
    expect(sel).toBeEnabled()
  })

  it('saves a pick through coder set --default-access and shows the returned value', async () => {
    App.CoderStatus.mockResolvedValue(status())
    App.CoderSetDefaultAccess.mockResolvedValue(status({ defaultAccess: 'coding' }))
    render(<CoderModeSection />)
    fireEvent.change(await screen.findByLabelText('Default access for new chats'), { target: { value: 'coding' } })
    await waitFor(() => expect(App.CoderSetDefaultAccess).toHaveBeenCalledWith('coding'))
    await waitFor(() => expect(screen.getByLabelText('Default access for new chats')).toHaveValue('coding'))
    expect(screen.queryByTestId('coder-default-allows')).toBeNull()
  })

  it('is disabled with the reason while coder is off', async () => {
    App.CoderStatus.mockResolvedValue(status({ enabled: false, defaultAccess: 'scoped' }))
    render(<CoderModeSection />)
    const sel = await screen.findByLabelText('Default access for new chats')
    expect(sel).toBeDisabled()
    expect(screen.getByText('Coder mode is off, so new chats start Scoped.')).toBeInTheDocument()
  })
})
