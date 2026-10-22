// @vitest-environment jsdom
import React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, cleanup } from '@testing-library/react'
import StatusBar from './StatusBar.jsx'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key) => key }),
}))

vi.mock('../lib/health.js', () => ({
  getHealth: () => ({ report: null, cliMissing: false }),
  subscribeHealth: () => () => {},
  summarize: () => ({ level: 'ok', issues: 0 }),
}))

vi.mock('../wailsjs/go/main/App', () => ({
  GetVersion: () => Promise.resolve({ version: '1.0.0' }),
  CheckForUpdate: () => Promise.resolve({ update_available: false }),
  AppSelfUpdate: () => Promise.resolve({ success: true }),
}))

vi.mock('../services/api.js', () => ({
  subscribeEvent: () => () => {},
}))

describe('StatusBar HIL toggle', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    cleanup()
  })

  it('renders HIL toggle next to the new-chat icon and handles click', async () => {
    const { CoderBubbleContext } = await import('./bubbles/CoderBubbleContext.jsx')
    const onToggleHil = vi.fn()
    const openNewChat = vi.fn()

    render(
      <CoderBubbleContext.Provider value={{ openDraft: null, openNewChat, navigate: null }}>
        <StatusBar
          stats={{ total_workflows: 5, total_people: 10, active_sessions: 1 }}
          dbConnected={true}
          hilOpen={false}
          hilCount={3}
          onToggleHil={onToggleHil}
        />
      </CoderBubbleContext.Provider>
    )

    const hilBtn = screen.getByTestId('hil-status-toggle')
    expect(hilBtn).toBeInTheDocument()
    expect(screen.getByText('3')).toBeInTheDocument()

    fireEvent.click(hilBtn)
    expect(onToggleHil).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByTitle('New chat'))
    expect(openNewChat).toHaveBeenCalledTimes(1)
    expect(openNewChat).toHaveBeenCalledWith()
  })
})

describe('StatusBar new-chat button', () => {
  afterEach(() => cleanup())

  it('has an accessible name and shows no dead button without the bubble store', async () => {
    render(<StatusBar stats={{}} dbConnected={true} />)
    expect(screen.queryByLabelText('New chat')).toBeNull()
    cleanup()
    const { CoderBubbleContext } = await import('./bubbles/CoderBubbleContext.jsx')
    render(
      <CoderBubbleContext.Provider value={{ openDraft: null, openNewChat: vi.fn(), navigate: null }}>
        <StatusBar stats={{}} dbConnected={true} />
      </CoderBubbleContext.Provider>,
    )
    expect(screen.getByLabelText('New chat')).toBeInTheDocument()
  })
})
