// @vitest-environment jsdom
import React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'
import Applications from './Applications.jsx'
import * as WailsApp from '../wailsjs/go/main/App'
import { mockApplicationDetail, mockApplicationAssets } from '../components/applications/fixtures.js'

vi.mock('../wailsjs/go/main/App', () => ({
  GetApplications: vi.fn(),
  GetApplication: vi.fn(),
  GetApplicationAssets: vi.fn(),
  AddApplication: vi.fn(),
  SetApplicationStatus: vi.fn(),
  OpenURL: vi.fn(),
  OpenPathWithOS: vi.fn(),
  GetProfileDocumentData: vi.fn(() => Promise.resolve('data:application/pdf;base64,test')),
  GetProfileDocumentText: vi.fn(() => Promise.resolve('test text')),
}))

vi.mock('../components/ConfirmDialog.jsx', () => ({
  confirm: vi.fn(() => Promise.resolve(true)),
}))

const sampleApps = [
  {
    id: 'app-1',
    title: 'Lead Systems Architect',
    company: 'OmniCorp',
    kind: 'job',
    status: 'pending',
    tags: ['go', 'react'],
    updated_at: '2026-10-22 12:00:00',
  },
  {
    id: 'app-2',
    title: 'Senior Frontend Engineer',
    company: 'Beta Corp',
    kind: 'job',
    status: 'awaiting_submit',
    tags: ['react'],
    updated_at: '2026-10-22 13:00:00',
  },
  {
    id: 'app-3',
    title: 'AI Researcher',
    company: 'Gamma Labs',
    kind: 'job',
    status: 'needs_you',
    tags: ['python'],
    updated_at: '2026-10-22 14:00:00',
  },
]

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('Applications page render', () => {
  beforeEach(() => {
    WailsApp.GetApplications.mockResolvedValue(sampleApps)
    WailsApp.GetApplication.mockResolvedValue(mockApplicationDetail)
    WailsApp.GetApplicationAssets.mockResolvedValue(mockApplicationAssets)
  })

  it('renders application list with status chips including awaiting_submit and needs_you on "all" tab', async () => {
    render(<Applications />)

    await waitFor(() => {
      expect(screen.getByText('Lead Systems Architect')).toBeInTheDocument()
    })

    // Switch to "all" tab to see awaiting_submit and needs_you items
    fireEvent.click(screen.getByRole('button', { name: 'all' }))

    await waitFor(() => {
      expect(screen.getByText('Senior Frontend Engineer')).toBeInTheDocument()
    })
    // The filter tab and the row badge say the same thing, without the underscore.
    expect(screen.getAllByText('awaiting submit')).toHaveLength(2)
    expect(screen.getAllByText('needs you')).toHaveLength(2)
  })

  it('opens application detail view on row click and renders all sections', async () => {
    const onNavigate = vi.fn()
    const onOpenCoderChat = vi.fn()

    render(<Applications onNavigate={onNavigate} onOpenCoderChat={onOpenCoderChat} />)

    await waitFor(() => {
      expect(screen.getByText('Lead Systems Architect')).toBeInTheDocument()
    })

    // Click the first row
    fireEvent.click(screen.getByText('Lead Systems Architect'))

    // Expect detail modal to open and load detail
    await waitFor(() => {
      expect(WailsApp.GetApplication).toHaveBeenCalledWith('app-1')
      expect(screen.getByRole('dialog', { name: /Application detail: Lead Systems Architect/i })).toBeInTheDocument()
    })

    // Posting section
    await waitFor(() => {
      expect(screen.getByText(/We are seeking a Lead Systems Architect/)).toBeInTheDocument()
    })

    // Documents section
    expect(screen.getByText('CV_Systems_Architect.pdf')).toBeInTheDocument()

    // Filled form section
    expect(screen.getByAltText('Filled form screenshot')).toBeInTheDocument()

    // People & messages section
    expect(screen.getByText('Sarah Connor')).toBeInTheDocument()

    // Tasks section
    expect(screen.getByText('Review OmniCorp submission form in parked browser tab')).toBeInTheDocument()

    // Coder task section
    expect(screen.getByRole('status', { name: /Coder task status: needs user/i })).toBeInTheDocument()

    // Close detail modal
    fireEvent.click(screen.getByRole('button', { name: 'Close dialog' }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: /Application detail/i })).not.toBeInTheDocument()
    })
  })

  it('opens the application a link from another page names (History drawer)', async () => {
    render(<Applications navData={{ application: 'app-1' }} />)

    await waitFor(() => {
      expect(WailsApp.GetApplication).toHaveBeenCalledWith('app-1')
      expect(screen.getByRole('dialog', { name: /Application detail/i })).toBeInTheDocument()
    })
  })

  it('does not open a detail without a link or while the page is hidden', async () => {
    render(<Applications isActive={false} navData={{ application: 'app-1' }} />)
    await waitFor(() => expect(WailsApp.GetApplications).toHaveBeenCalled())
    expect(WailsApp.GetApplication).not.toHaveBeenCalled()
  })

  it('revert-proof: guards against missing GetApplicationAssets binding when opening detail', async () => {
    // Delete the Go binding to simulate it not yet existing
    const orig = WailsApp.GetApplicationAssets
    delete WailsApp.GetApplicationAssets

    try {
      render(<Applications />)

      await waitFor(() => {
        expect(screen.getByText('Lead Systems Architect')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByText('Lead Systems Architect'))

      // Detail still opens without crashing and shows empty states for asset sections
      await waitFor(() => {
        expect(screen.getByRole('dialog', { name: /Application detail: Lead Systems Architect/i })).toBeInTheDocument()
      })

      await waitFor(() => {
        expect(screen.getByText('No captured posting text available.')).toBeInTheDocument()
      })
      expect(screen.getByText('No documents attached or generated for this application.')).toBeInTheDocument()
      expect(screen.getByText('No filled form data available.')).toBeInTheDocument()
      expect(screen.getByText('No active coder task.')).toBeInTheDocument()
    } finally {
      WailsApp.GetApplicationAssets = orig
    }
  })

  it('keeps Process Pending flow and Add modal working unchanged', async () => {
    render(<Applications />)

    await waitFor(() => {
      expect(screen.getByText('Lead Systems Architect')).toBeInTheDocument()
    })

    // Click Process Pending
    fireEvent.click(screen.getByRole('button', { name: /Process Pending/i }))
    expect(screen.getByText(/Process Pending Applications/i)).toBeInTheDocument()

    // Close Process Pending flow by clicking Cancel button
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    // Click Add button
    fireEvent.click(screen.getByRole('button', { name: /Add/i }))
    expect(screen.getByRole('heading', { name: 'Add Application' })).toBeInTheDocument()
  })
})
