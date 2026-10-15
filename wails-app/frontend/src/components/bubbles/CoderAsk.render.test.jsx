// @vitest-environment jsdom
// A coder bubble shows the sends its agents prepared as the same AskCard the
// org bubble uses, and nothing is sent until the person approves.
import React, { useEffect } from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup, within } from '@testing-library/react'
import '../../i18n.js'
import sendEmail from './__fixtures__/hil-send-email.json'
import workflow from './__fixtures__/hil-workflow.json'

const { api } = vi.hoisted(() => ({
  api: {
    getChatTurns: vi.fn(), getChatEvents: vi.fn(), startChatTurn: vi.fn(), stopChatTurn: vi.fn(), coderStatus: vi.fn(),
    coderWorkspaceList: vi.fn(), coderWorkspaceRoot: vi.fn(), getAgentRuntimeModels: vi.fn(),
    hilList: vi.fn(), hilResolve: vi.fn(), hilSendResolve: vi.fn(), hilSendAttachmentData: vi.fn(), openPathWithOS: vi.fn(), openURL: vi.fn(),
  },
}))
vi.mock('../../services/api.js', () => ({
  api,
  notify: vi.fn(),
  onChatEvent: () => () => {},
  onOrgEvent: () => () => {},
  onOrgRunStatus: () => () => {},
  newOrgEventsStreamId: () => 'stream',
}))
vi.mock('../ConfirmDialog.jsx', () => ({ confirm: vi.fn() }))

import CoderBubbles from './CoderBubbles.jsx'
import { useCoderBubbles } from './useCoderBubbles.js'

function Harness({ open }) {
  const store = useCoderBubbles()
  useEffect(() => { store.openConversation(open) }, []) // eslint-disable-line react-hooks/exhaustive-deps
  return <CoderBubbles store={store} onNavigate={() => {}} />
}
const conv = { id: 'c1', cwd: '/w/mono-agent', model: 'opus' }

beforeEach(() => {
  vi.clearAllMocks()
  Element.prototype.scrollTo = function scrollTo() {}
  localStorage.clear()
  api.getChatTurns.mockResolvedValue({ items: [] })
  api.getChatEvents.mockResolvedValue({ items: [], hasMore: false })
  api.coderStatus.mockResolvedValue({ enabled: true, ready: true, workspaceRoot: '/w' })
  api.coderWorkspaceList.mockResolvedValue([])
  api.getAgentRuntimeModels.mockResolvedValue([])
  api.hilList.mockResolvedValue([sendEmail, { ...sendEmail, id: 'for-other', conversationId: 'c2' }, workflow])
  api.hilResolve.mockResolvedValue({ ok: true, state: 'approved' })
  api.hilSendAttachmentData.mockResolvedValue('data:image/png;base64,AAAA')
  api.hilSendResolve.mockResolvedValue({ ok: true })
})
afterEach(cleanup)

describe('asks in a coder bubble', () => {
  it('shows this chat\'s send approvals, and not another chat\'s or an org\'s workflow item', async () => {
    render(<Harness open={conv} />)
    const region = await screen.findByTestId('coder-asks')
    expect(region).toHaveAttribute('aria-live', 'polite')
    const cards = within(region).getAllByTestId('org-chat-send')
    expect(cards).toHaveLength(1)
    expect(cards[0]).toHaveTextContent('Sam Rivera')
    expect(within(region).queryByTestId('org-chat-hil')).toBeNull()
  })

  it('approves with an edit through the HIL queue, and rejects', async () => {
    render(<Harness open={conv} />)
    const card = await screen.findByTestId('org-chat-send')
    fireEvent.click(within(card).getByTestId('ask-edit'))
    fireEvent.change(within(card).getByTestId('ask-text-edit'), { target: { value: 'Hello' } })
    fireEvent.click(within(card).getByTestId('ask-approve'))
    await waitFor(() => expect(api.hilSendResolve).toHaveBeenCalledWith('hil-send-1', true, { editText: 'Hello' }))
    await waitFor(() => expect(screen.getByTestId('org-chat-send')).toHaveAttribute('data-pending', 'false'))
  })

  it('rejects without sending anything', async () => {
    render(<Harness open={conv} />)
    const card = await screen.findByTestId('org-chat-send')
    fireEvent.click(within(card).getByTestId('ask-reject'))
    await waitFor(() => expect(api.hilSendResolve).toHaveBeenCalledWith('hil-send-1', false, { editText: '' }))
  })
})
