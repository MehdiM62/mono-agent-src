// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/react'
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (k) => k }) }))
vi.mock('../wailsjs/go/main/App', () => ({ GetVersion: () => Promise.resolve({ version: 'v1.2.3' }) }))
vi.mock('../services/api.js', () => ({ api: {}, PLATFORM_COLORS: {}, subscribeEvent: () => () => {} }))
vi.mock('../lib/health.js', () => ({
  getHealth: () => ({ report: { results: [] } }),
  subscribeHealth: () => () => {},
  summarize: () => ({ level: 'ok', issues: 0 }),
  runHealth: vi.fn(),
}))
vi.mock('../components/NewProfileModal.jsx', () => ({
  default: ({ open }) => (open ? <div>new-profile-modal</div> : null),
}))
vi.mock('./dashboard/useDashboardData.js', () => ({
  useDashboardData: () => ({
    loading: false, noProfile: true, summary: null, summaryFailed: false, orgs: null,
    workflows: [], executions: [], refresh: vi.fn(), setExecutions: vi.fn(), reloadLists: vi.fn(),
  }),
}))
import Dashboard from './Dashboard.jsx'

afterEach(cleanup)

describe('Dashboard with no profile', () => {
  it('shows the empty state with a New profile action instead of a failed summary', () => {
    render(<Dashboard onNavigate={vi.fn()} onOpenHil={vi.fn()} onRefresh={vi.fn()} />)
    expect(screen.getByTestId('dash-no-profile')).toBeInTheDocument()
    expect(screen.queryByText('dashboard.attention.failed')).toBeNull()
    expect(screen.queryByText('dashboard.workflowsSection.title')).toBeNull()
    fireEvent.click(screen.getByText('dashboard.noProfile.action'))
    expect(screen.getByText('new-profile-modal')).toBeInTheDocument()
  })
})
