// @vitest-environment jsdom
import React, { useEffect } from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup, within } from '@testing-library/react'
import '../../i18n.js'

// Ported from the assistant panel: result cards for what a tool call made, the
// "set up AI agents" link on a turn that failed because no agent is set up.
const { api } = vi.hoisted(() => ({
  api: {
    getChatTurns: vi.fn(), getChatEvents: vi.fn(), startChatTurn: vi.fn(), stopChatTurn: vi.fn(), coderStatus: vi.fn(),
    coderWorkspaceList: vi.fn(), getAgentRuntimeModels: vi.fn(), getProfiles: vi.fn(), listChatConversations: vi.fn(),
    browserPermissions: vi.fn(), listOrgDesigns: vi.fn(), getWorkflow: vi.fn(),
  },
}))
vi.mock('../../services/api.js', () => ({
  api, notify: vi.fn(), onChatEvent: () => () => {}, onOrgEvent: () => () => {}, onOrgRunStatus: () => () => {}, newOrgEventsStreamId: () => 's',
}))
vi.mock('../../lib/chatAccessApi.js', () => ({ chatAccessApi: { create: vi.fn(), setAccess: vi.fn() } }))
vi.mock('../ConfirmDialog.jsx', () => ({ confirm: vi.fn() }))

import CoderBubbles from './CoderBubbles.jsx'
import { useCoderBubbles } from './useCoderBubbles.js'

const onOpenArtifact = vi.fn()
const onNavigate = vi.fn()
function Harness() {
  const store = useCoderBubbles()
  useEffect(() => { store.openConversation({ id: 's1', access: 'scoped', runtimeId: 'claude', model: 'm' }) }, []) // eslint-disable-line react-hooks/exhaustive-deps
  return <CoderBubbles store={store} onNavigate={onNavigate} onOpenArtifact={onOpenArtifact} />
}
const ev = (seq, type, payload) => ({ conversationId: 's1', turnId: 't1', seq, type, payload, at: '2026-10-22T10:00:00Z' })

beforeEach(() => {
  vi.clearAllMocks()
  Element.prototype.scrollTo = function scrollTo() {}
  localStorage.clear()
  api.getChatTurns.mockResolvedValue({ items: [{ id: 't1', prompt: 'make an org', status: 'completed', ownedByThisInstance: true }] })
  api.coderStatus.mockResolvedValue({ enabled: false })
  api.coderWorkspaceList.mockResolvedValue([])
  api.getProfiles.mockResolvedValue([])
  api.listChatConversations.mockResolvedValue({ items: [] })
  api.browserPermissions.mockResolvedValue({ kinds: [], catalog: [] })
  api.listOrgDesigns.mockResolvedValue([{ name: 'acme' }])
})
afterEach(cleanup)

describe('a finished turn in a bubble', () => {
  it('shows a card for the org a tool call created and opens it through the app', async () => {
    api.getChatEvents.mockResolvedValue({ hasMore: false, lastCommittedSeq: 4, items: [
      ev(1, 'turn.started', {}),
      ev(2, 'tool.started', { callId: 'c1', name: 'create_org', arguments: '{}' }),
      ev(3, 'tool.completed', { callId: 'c1', ok: true, result: JSON.stringify({ org_name: 'acme' }) }),
      ev(4, 'turn.finished', { status: 'completed' }),
    ] })
    render(<Harness />)
    const open = await screen.findByRole('button', { name: 'Open organization' })
    expect(within(open.parentElement).getByText('acme')).toBeInTheDocument()
    fireEvent.click(open)
    await waitFor(() => expect(onOpenArtifact).toHaveBeenCalledWith({ type: 'org', name: 'acme' }))
  })

  it('shows no card for a tool call that made nothing, or whose org no longer exists', async () => {
    api.listOrgDesigns.mockResolvedValue([])
    api.getChatEvents.mockResolvedValue({ hasMore: false, lastCommittedSeq: 4, items: [
      ev(1, 'turn.started', {}),
      ev(2, 'tool.started', { callId: 'c1', name: 'create_org', arguments: '{}' }),
      ev(3, 'tool.completed', { callId: 'c1', ok: true, result: JSON.stringify({ org_name: 'gone' }) }),
      ev(4, 'turn.finished', { status: 'completed' }),
    ] })
    render(<Harness />)
    await waitFor(() => expect(api.listOrgDesigns).toHaveBeenCalled())
    await screen.findAllByText('make an org')
    expect(screen.queryByRole('button', { name: 'Open organization' })).toBeNull()
  })

  it('offers the AI agents page when the turn failed because no agent is set up', async () => {
    api.getChatEvents.mockResolvedValue({ hasMore: false, lastCommittedSeq: 2, items: [
      ev(1, 'turn.started', {}),
      ev(2, 'turn.finished', { status: 'failed', code: 'agent_not_setup', message: 'The AI agent isn\'t set up.' }),
    ] })
    api.getChatTurns.mockResolvedValue({ items: [{ id: 't1', prompt: 'hi', status: 'failed', ownedByThisInstance: true }] })
    render(<Harness />)
    const link = await screen.findByRole('button', { name: /set up ai agents/i })
    fireEvent.click(link)
    expect(onNavigate).toHaveBeenCalled()
  })
})
