// @vitest-environment jsdom
import React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'
import Sidebar from './Sidebar.jsx'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key) => key }),
}))

const { getProfiles } = vi.hoisted(() => ({ getProfiles: vi.fn() }))

vi.mock('../wailsjs/go/main/App', () => ({
  GetVersion: () => Promise.resolve({ version: '1.0.0' }),
  GetHILItems: () => Promise.resolve([]),
  GetProfiles: () => getProfiles(),
  SwitchProfile: () => Promise.resolve(),
  ChooseProfileFolder: () => Promise.resolve(''),
  MoveProfileFolder: () => Promise.resolve(),
  RevealProfileFolder: () => Promise.resolve(),
}))

// Isolates this test from NewProfileModal's own implementation (covered by
// NewProfileModal.render.test.jsx) -- only Sidebar's own decision to open it
// is under test here.
vi.mock('./NewProfileModal.jsx', () => ({
  default: ({ open }) => (open ? <div data-testid="new-profile-modal">new profile modal open</div> : null),
}))

beforeEach(() => {
  vi.clearAllMocks()
  getProfiles.mockResolvedValue([{ id: 'default', name: 'Default', is_active: true, root_dir: '', icon: '' }])
})

afterEach(() => {
  cleanup()
})

// Regression test for replacing the old inline "+ New profile" expand-in-
// place row with a centered modal: clicking the profile switcher then
// "+ New profile" must open NewProfileModal, not an inline form.
it('opens the New Profile modal when "+ New profile" is clicked', async () => {
  render(<Sidebar activePage="dashboard" onNavigate={() => {}} stats={{}} dbConnected={true} />)

  await waitFor(() => expect(screen.getByText('Default')).toBeInTheDocument())

  expect(screen.queryByTestId('new-profile-modal')).not.toBeInTheDocument()

  fireEvent.click(screen.getByText('Default'))
  fireEvent.click(screen.getByText('New profile'))

  expect(screen.getByTestId('new-profile-modal')).toBeInTheDocument()
})

it('does not render AI agents tab in sidebar navigation', async () => {
  render(<Sidebar activePage="dashboard" onNavigate={() => {}} stats={{}} dbConnected={true} />)

  expect(screen.getByText('sidebar.nav.dashboard')).toBeInTheDocument()
  expect(screen.getByText('sidebar.nav.orgs')).toBeInTheDocument()
  expect(screen.getByText('sidebar.nav.settings')).toBeInTheDocument()
  expect(screen.queryByText('sidebar.nav.ai')).not.toBeInTheDocument()
})


// The window's own selection is the only source of the shown profile: with
// no profile at all nothing is named "Default"; the switcher offers a new one.
it('names no profile when none exists', async () => {
  getProfiles.mockResolvedValue([])
  render(<Sidebar activePage="dashboard" onNavigate={() => {}} stats={{}} dbConnected={true} />)

  await waitFor(() => expect(getProfiles).toHaveBeenCalled())
  expect(screen.queryByText('Default')).not.toBeInTheDocument()
  expect(screen.getByText('New profile')).toBeInTheDocument()
})

it('History opens the history drawer instead of navigating', async () => {
  const onNavigate = vi.fn()
  const seen = vi.fn()
  const { onHistoryRequest } = await import('./history/historyBus.js')
  const off = onHistoryRequest(seen)
  render(<Sidebar activePage="dashboard" onNavigate={onNavigate} stats={{}} dbConnected={true} />)
  fireEvent.click(screen.getByText('sidebar.nav.history'))
  expect(seen).toHaveBeenCalledWith('open')
  expect(onNavigate).not.toHaveBeenCalled()
  off()
})
