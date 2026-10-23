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

  it('renders HIL toggle next to the history icon and handles click', async () => {
    const { onHistoryRequest } = await import('./history/historyBus.js')
    const seen = vi.fn()
    const off = onHistoryRequest(seen)
    const onToggleHil = vi.fn()

    render(
      <StatusBar
        stats={{ total_workflows: 5, total_people: 10, active_sessions: 1 }}
        dbConnected={true}
        hilOpen={false}
        hilCount={3}
        onToggleHil={onToggleHil}
      />
    )

    const hilBtn = screen.getByTestId('hil-status-toggle')
    expect(hilBtn).toBeInTheDocument()
    expect(screen.getByText('3')).toBeInTheDocument()

    fireEvent.click(hilBtn)
    expect(onToggleHil).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByTestId('history-status-toggle'))
    expect(seen).toHaveBeenCalledWith('toggle')
    off()
  })
})

describe('StatusBar history button', () => {
  afterEach(() => cleanup())

  it('is always there, has an accessible name, and replaces the old chat icon', () => {
    render(<StatusBar stats={{}} dbConnected={true} />)
    const btn = screen.getByLabelText('history.title')
    expect(btn).toBeInTheDocument()
    expect(btn).toHaveAttribute('aria-haspopup', 'dialog')
    expect(screen.queryByLabelText('New chat')).toBeNull()
    expect(screen.queryByTitle('New chat')).toBeNull()
  })
})
