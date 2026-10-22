// @vitest-environment jsdom
import React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, cleanup } from '@testing-library/react'
import ApplicationHeader from './ApplicationHeader.jsx'
import ApplicationPostingSection from './ApplicationPostingSection.jsx'
import ApplicationDocumentsSection from './ApplicationDocumentsSection.jsx'
import ApplicationFillSection from './ApplicationFillSection.jsx'
import ApplicationPeopleMessagesSection from './ApplicationPeopleMessagesSection.jsx'
import ApplicationTasksSection from './ApplicationTasksSection.jsx'
import ApplicationStatusHistorySection from './ApplicationStatusHistorySection.jsx'
import ApplicationCoderTaskSection from './ApplicationCoderTaskSection.jsx'
import * as WailsApp from '../../wailsjs/go/main/App'
import { mockApplicationDetail, mockApplicationAssets } from './fixtures.js'

vi.mock('../../wailsjs/go/main/App', () => ({
  OpenURL: vi.fn(),
  GetApplicationAssets: vi.fn(),
}))

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('ApplicationHeader', () => {
  it('renders title, company, deadline, and posting URL', () => {
    const onSetStatus = vi.fn()
    const onClose = vi.fn()

    render(
      <ApplicationHeader
        detail={mockApplicationDetail}
        assets={mockApplicationAssets}
        onSetStatus={onSetStatus}
        onClose={onClose}
      />
    )

    expect(screen.getByText('Lead Systems Architect')).toBeInTheDocument()
    expect(screen.getByText('OmniCorp')).toBeInTheDocument()
    expect(screen.getByText(/Deadline: 2026-11-15/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Open link/ })).toBeInTheDocument()

    // Test OpenURL click
    fireEvent.click(screen.getByRole('link', { name: /Open link/ }))
    expect(WailsApp.OpenURL).toHaveBeenCalledWith(mockApplicationDetail.url)
  })

  it('renders status chip for awaiting_submit state and allows cancel', () => {
    const onSetStatus = vi.fn()
    render(
      <ApplicationHeader
        detail={{ ...mockApplicationDetail, status: 'awaiting_submit' }}
        assets={mockApplicationAssets}
        onSetStatus={onSetStatus}
      />
    )

    expect(screen.getByRole('status', { name: /Status: awaiting submit/i })).toBeInTheDocument()
    const cancelBtn = screen.getByRole('button', { name: /Cancel this application/i })
    expect(cancelBtn).toBeInTheDocument()
    fireEvent.click(cancelBtn)
    expect(onSetStatus).toHaveBeenCalledWith('app-test-1', 'cancelled')
  })

  it('renders status chip for needs_you state and allows cancel', () => {
    const onSetStatus = vi.fn()
    render(
      <ApplicationHeader
        detail={{ ...mockApplicationDetail, status: 'needs_you' }}
        assets={mockApplicationAssets}
        onSetStatus={onSetStatus}
      />
    )

    expect(screen.getByRole('status', { name: /Status: needs you/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Cancel this application/i })).toBeInTheDocument()
  })

  it('does not show cancel button when already applied or rejected', () => {
    render(
      <ApplicationHeader
        detail={{ ...mockApplicationDetail, status: 'applied' }}
        assets={mockApplicationAssets}
        onSetStatus={vi.fn()}
      />
    )
    expect(screen.queryByRole('button', { name: /Cancel this application/i })).not.toBeInTheDocument()
  })
})

describe('ApplicationPostingSection', () => {
  it('renders captured text and link', () => {
    render(<ApplicationPostingSection posting={mockApplicationAssets.posting} />)

    expect(screen.getByText(/We are seeking a Lead Systems Architect/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Open original job posting/i })).toBeInTheDocument()
    expect(screen.getByText(/Captured: 2026-10-22/)).toBeInTheDocument()
  })

  it('renders empty state when no posting data is available', () => {
    render(<ApplicationPostingSection posting={null} />)
    expect(screen.getByText('No captured posting text available.')).toBeInTheDocument()
  })
})

describe('ApplicationDocumentsSection', () => {
  it('renders documents with attached badge and kind/source metadata', () => {
    const onPreviewDoc = vi.fn()
    render(
      <ApplicationDocumentsSection
        documents={mockApplicationAssets.documents}
        onPreviewDoc={onPreviewDoc}
      />
    )

    expect(screen.getByText('CV_Systems_Architect.pdf')).toBeInTheDocument()
    expect(screen.getByText('Cover_Letter_OmniCorp.html')).toBeInTheDocument()
    expect(screen.getByText('Architecture_Portfolio.pdf')).toBeInTheDocument()

    // Attached badge
    const attachedBadges = screen.getAllByRole('status', { name: /Attached to application/i })
    expect(attachedBadges).toHaveLength(2)

    // Preview button
    const previewButtons = screen.getAllByRole('button', { name: /Preview/i })
    expect(previewButtons).toHaveLength(3)
    fireEvent.click(previewButtons[0])
    expect(onPreviewDoc).toHaveBeenCalledWith(mockApplicationAssets.documents[0])
  })

  it('renders empty state when documents array is empty', () => {
    render(<ApplicationDocumentsSection documents={[]} />)
    expect(screen.getByText('No documents attached or generated for this application.')).toBeInTheDocument()
  })
})

describe('ApplicationFillSection', () => {
  it('renders fill status, screenshot, and field report table with generated badges', () => {
    render(<ApplicationFillSection fill={mockApplicationAssets.fill} />)

    expect(screen.getByRole('status', { name: /Fill status: filled/i })).toBeInTheDocument()
    expect(screen.getByAltText('Filled form screenshot')).toBeInTheDocument()
    expect(screen.getByText('Full Legal Name')).toBeInTheDocument()
    expect(screen.getByText('Alex Morgan')).toBeInTheDocument()
    expect(screen.getByText('Why do you want to join OmniCorp?')).toBeInTheDocument()

    // 'generated' badge for the AI field
    expect(screen.getByRole('status', { name: /Generated by AI/i })).toBeInTheDocument()
  })

  it('renders empty state when fill object is null or has no fields', () => {
    render(<ApplicationFillSection fill={null} />)
    expect(screen.getByText('No filled form data available.')).toBeInTheDocument()
  })
})

describe('ApplicationPeopleMessagesSection', () => {
  it('renders recruiter contacts and navigates to Communications tab', () => {
    const onNavigate = vi.fn()
    render(
      <ApplicationPeopleMessagesSection
        people={mockApplicationAssets.people}
        messages={mockApplicationAssets.messages}
        onNavigate={onNavigate}
      />
    )

    expect(screen.getByText('Sarah Connor')).toBeInTheDocument()
    expect(screen.getByText('Staff Talent Partner')).toBeInTheDocument()
    expect(screen.getByText('sconnor@omnicorp.example')).toBeInTheDocument()

    const commsBtn = screen.getByRole('button', { name: /Open communications for Sarah Connor/i })
    fireEvent.click(commsBtn)
    expect(onNavigate).toHaveBeenCalledWith('communications', {
      person: 'person-sarah-1',
      personName: 'Sarah Connor',
    })
  })

  it('renders message threads and navigates to Communications tab', () => {
    const onNavigate = vi.fn()
    render(
      <ApplicationPeopleMessagesSection
        people={mockApplicationAssets.people}
        messages={mockApplicationAssets.messages}
        onNavigate={onNavigate}
      />
    )

    expect(screen.getByText(/Interview inquiry/)).toBeInTheDocument()
    expect(screen.getByText(/we were impressed by your background/)).toBeInTheDocument()
    expect(screen.getByText(/Incoming/i)).toBeInTheDocument()

    const threadBtn = screen.getByRole('button', { name: /View thread in communications/i })
    fireEvent.click(threadBtn)
    expect(onNavigate).toHaveBeenCalledWith('communications', {
      messageId: 'msg-thread-101',
      hil_ref: 'msg-thread-101',
      person: undefined,
    })
  })

  it('renders empty states when lists are empty', () => {
    render(<ApplicationPeopleMessagesSection people={[]} messages={[]} />)
    expect(screen.getByText('No people or messages linked to this application.')).toBeInTheDocument()
  })
})

describe('ApplicationTasksSection', () => {
  it('renders board task items and navigates when clicked', () => {
    const onNavigate = vi.fn()
    render(<ApplicationTasksSection tasks={mockApplicationAssets.tasks} onNavigate={onNavigate} />)

    expect(screen.getByText('Review OmniCorp submission form in parked browser tab')).toBeInTheDocument()
    expect(screen.getByRole('status', { name: /Task status: ready/i })).toBeInTheDocument()

    const viewBtn = screen.getByRole('button', { name: /Open task board/i })
    fireEvent.click(viewBtn)
    expect(onNavigate).toHaveBeenCalledWith('tasks', { taskId: 'task-board-1' })
  })

  it('renders empty state when tasks array is empty', () => {
    render(<ApplicationTasksSection tasks={[]} />)
    expect(screen.getByText('No tasks linked to this application.')).toBeInTheDocument()
  })
})

describe('ApplicationStatusHistorySection', () => {
  it('renders chronological log of status transitions', () => {
    render(<ApplicationStatusHistorySection statusLog={mockApplicationDetail.status_log} />)

    expect(screen.getByText('(created)')).toBeInTheDocument()
    expect(screen.getByText('awaiting_submit')).toBeInTheDocument()
    expect(screen.getByText('user')).toBeInTheDocument()
    expect(screen.getByText('agent')).toBeInTheDocument()
  })

  it('renders empty state when status log is empty', () => {
    render(<ApplicationStatusHistorySection statusLog={[]} />)
    expect(screen.getByText('No status history recorded.')).toBeInTheDocument()
  })
})

describe('ApplicationCoderTaskSection', () => {
  it('renders task status and enables "Open chat" to open coder bubble', () => {
    const onOpenCoderChat = vi.fn()
    render(
      <ApplicationCoderTaskSection
        task={mockApplicationAssets.task}
        onOpenCoderChat={onOpenCoderChat}
      />
    )

    expect(screen.getByRole('status', { name: /Coder task status: needs user/i })).toBeInTheDocument()
    expect(screen.getByText('aitask-app-apply-992')).toBeInTheDocument()
    expect(screen.getByText('conv-coder-8812')).toBeInTheDocument()

    const openChatBtn = screen.getByRole('button', { name: /Open chat in coder bubble/i })
    expect(openChatBtn).not.toBeDisabled()
    fireEvent.click(openChatBtn)
    expect(onOpenCoderChat).toHaveBeenCalledWith({ id: 'conv-coder-8812' })
  })

  it('renders "Reopen tab" and "Refill" as disabled buttons with tooltip "coming with the apply task"', () => {
    render(<ApplicationCoderTaskSection task={mockApplicationAssets.task} />)

    const reopenBtn = screen.getByRole('button', { name: /Reopen tab \(coming with the apply task\)/i })
    expect(reopenBtn).toBeDisabled()
    expect(reopenBtn).toHaveAttribute('title', 'coming with the apply task')

    const refillBtn = screen.getByRole('button', { name: /Refill \(coming with the apply task\)/i })
    expect(refillBtn).toBeDisabled()
    expect(refillBtn).toHaveAttribute('title', 'coming with the apply task')
  })

  it('renders empty state when task is null', () => {
    render(<ApplicationCoderTaskSection task={null} />)
    expect(screen.getByText('No active coder task.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Open chat in coder bubble/i })).toBeDisabled()
  })
})
