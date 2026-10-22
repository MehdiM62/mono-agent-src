// @vitest-environment jsdom
import React, { useEffect } from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup, act, within } from '@testing-library/react'
import '../../i18n.js'

// Behaviours of the old assistant panel that live on in the bubble pane: a
// reopened transcript (interrupted calls, a turn another window owns), notices
// announced once, stop failing, "Jump to latest", result-card revalidation and
// the model list of a runtime that is still loading.
const { api, listeners, chatAccess, scan } = vi.hoisted(() => ({
  listeners: { chat: new Set() },
  chatAccess: { create: vi.fn(), setAccess: vi.fn(), record: vi.fn() },
  scan: vi.fn(),
  api: {
    getChatTurns: vi.fn(), getChatEvents: vi.fn(), startChatTurn: vi.fn(), stopChatTurn: vi.fn(), coderStatus: vi.fn(),
    coderWorkspaceList: vi.fn(), getAgentRuntimeModels: vi.fn(), getProfiles: vi.fn(), browserPermissions: vi.fn(), listOrgDesigns: vi.fn(),
  },
}))
vi.mock('../../services/api.js', () => ({
  api, notify: vi.fn(),
  onChatEvent: cb => { listeners.chat.add(cb); return () => listeners.chat.delete(cb) },
  onOrgEvent: () => () => {}, onOrgRunStatus: () => () => {}, newOrgEventsStreamId: () => 's',
}))
vi.mock('../../lib/chatAccessApi.js', () => ({ chatAccessApi: chatAccess }))
vi.mock('../../lib/agentRuntimes.js', async importOriginal => ({ ...(await importOriginal()), cachedAgentScan: (...a) => scan(...a) }))
vi.mock('../ConfirmDialog.jsx', () => ({ confirm: vi.fn() }))

import CoderBubbles from './CoderBubbles.jsx'
import { useCoderBubbles } from './useCoderBubbles.js'

const onOpenArtifact = vi.fn()
function Harness({ seed }) {
  const store = useCoderBubbles()
  useEffect(() => { if (seed === 'draft') store.openNewChat(); else store.openConversation({ id: 's1', access: 'scoped', runtimeId: 'claude', model: 'm' }) }, []) // eslint-disable-line react-hooks/exhaustive-deps
  return <CoderBubbles store={store} onNavigate={() => {}} onOpenArtifact={onOpenArtifact} />
}
const emit = e => act(() => { for (const cb of [...listeners.chat]) cb(e) })
const ev = (seq, type, payload) => ({ conversationId: 's1', turnId: 't1', seq, type, payload, at: '2026-09-12T00:00:0' + seq + 'Z' })
const box = () => within(screen.getByTestId('bubble-overlay')).getByPlaceholderText('Type a message...')
const live = () => screen.getByRole('status', { name: 'Chat turn announcements' })
const turns = (status, owned) => api.getChatTurns.mockResolvedValue({ items: [{ id: 't1', prompt: 'the prompt', status, ...(owned === undefined ? {} : { ownedByThisInstance: owned }) }] })
const events = items => api.getChatEvents.mockResolvedValue({ items, hasMore: false, lastCommittedSeq: items.length })
const activeEvents = [ev(1, 'turn.started', {}), ev(2, 'tool.started', { callId: 'c1', name: 'slow_tool', arguments: {} })]

async function sendHello() {
  fireEvent.change(box(), { target: { value: 'hello' } })
  fireEvent.keyDown(box(), { key: 'Enter' })
  await waitFor(() => expect(api.startChatTurn).toHaveBeenCalled())
  await waitFor(() => expect(listeners.chat.size).toBeGreaterThan(0))
  return api.startChatTurn.mock.calls[0][1]
}

beforeEach(() => {
  vi.clearAllMocks()
  Element.prototype.scrollTo = function scrollTo() {}
  listeners.chat.clear()
  localStorage.clear()
  api.getChatTurns.mockResolvedValue({ items: [] })
  api.getChatEvents.mockResolvedValue({ items: [], hasMore: false })
  api.coderStatus.mockResolvedValue({ enabled: false })
  api.coderWorkspaceList.mockResolvedValue([])
  api.getProfiles.mockResolvedValue([])
  api.browserPermissions.mockResolvedValue({ kinds: [], catalog: [] })
  api.listOrgDesigns.mockResolvedValue([])
  api.startChatTurn.mockResolvedValue({ ok: true })
  api.stopChatTurn.mockResolvedValue({ ok: true })
  api.getAgentRuntimeModels.mockResolvedValue([{ id: 'sonnet', label: 'Sonnet' }])
  scan.mockResolvedValue({ agents: [{ id: 'claude', installed: true, binary: '/bin/claude' }, { id: 'codex', installed: true, binary: '/bin/codex' }] })
  chatAccess.record.mockResolvedValue({ access: 'scoped', title: '' })
  chatAccess.create.mockResolvedValue({ id: 's1', access: 'scoped', runtimeId: 'claude', model: 'sonnet' })
})
afterEach(cleanup)

describe('a reopened transcript', () => {
  it('shows a call still "started" in a finished turn as Interrupted, not a live Running', async () => {
    turns('cancelled')
    events([ev(1, 'turn.started', {}), ev(2, 'tool.started', { callId: 'c1', name: 'slow_tool', arguments: {} }),
      ev(3, 'turn.finished', { status: 'cancelled', reason: 'stopped', exitCode: null, historySaved: true })])
    render(<Harness />)
    await screen.findByText(/Interrupted/i)
    expect(screen.queryByText(/^Running$/i)).not.toBeInTheDocument()
  })

  it('shows a static "Running in another window" for a turn another instance owns, with no spinner or Stop', async () => {
    turns('active', false)
    events(activeEvents)
    render(<Harness />)
    await screen.findByText('Running in another window')
    expect(screen.queryByText(/^Running slow_tool$/)).not.toBeInTheDocument()
    expect(document.querySelector('.chat-spin')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Stop generating' })).not.toBeInTheDocument()
  })

  it('still shows a foreign turn that has no observed parts yet, with its prompt', async () => {
    turns('active', false)
    events([ev(1, 'turn.started', {})])
    render(<Harness />)
    await screen.findAllByText('the prompt')
    await screen.findByText('Running in another window')
  })

  it('treats an active turn this window owns, or with no ownership field, as live', async () => {
    turns('active', true)
    events(activeEvents)
    render(<Harness />)
    await screen.findByText('Running slow_tool')
    expect(screen.queryByText('Running in another window')).not.toBeInTheDocument()
    cleanup()
    turns('active')
    render(<Harness />)
    await screen.findByText('Running slow_tool')
    expect(screen.queryByText('Running in another window')).not.toBeInTheDocument()
  })

  it('re-validates a result card at click time: an org deleted since refuses to open', async () => {
    turns('completed')
    events([ev(1, 'turn.started', {}), ev(2, 'tool.started', { callId: 'c1', name: 'create_org', arguments: '{}' }),
      ev(3, 'tool.completed', { callId: 'c1', ok: true, result: JSON.stringify({ org_name: 'Acme' }) }), ev(4, 'turn.finished', { status: 'completed' })])
    api.listOrgDesigns.mockResolvedValueOnce([{ name: 'Acme' }]).mockResolvedValueOnce([])
    render(<Harness />)
    fireEvent.click(await screen.findByTitle('Open organization'))
    await waitFor(() => expect(api.listOrgDesigns).toHaveBeenCalledTimes(2))
    expect(onOpenArtifact).not.toHaveBeenCalled()
  })
})

describe('a live turn', () => {
  it('announces a mid-turn notice at once, and not again in the finish summary', async () => {
    render(<Harness />)
    await waitFor(() => expect(box()).not.toBeDisabled())
    const turnId = await sendHello()
    const e = (seq, type, payload) => ({ conversationId: 's1', turnId, seq, type, payload, at: '2026-09-12T00:00:0' + seq + 'Z' })
    emit(e(1, 'notice', { code: 'rate_limited', message: 'Slowing down due to rate limits.', severity: 'warning' }))
    await waitFor(() => expect(live()).toHaveTextContent('Slowing down due to rate limits.'))
    expect(screen.getByRole('button', { name: 'Stop generating' })).toBeInTheDocument()
    emit(e(2, 'turn.finished', { status: 'completed', reason: 'end_turn', exitCode: 0, historySaved: true }))
    await waitFor(() => expect(live().textContent).toBe('Response completed.'))
  })

  it('recovers from a failed stop instead of staying on "Stopping"', async () => {
    api.stopChatTurn.mockRejectedValueOnce(new Error('chat supervisor not initialized'))
    render(<Harness />)
    await waitFor(() => expect(box()).not.toBeDisabled())
    await sendHello()
    fireEvent.click(await screen.findByRole('button', { name: 'Stop generating' }))
    await waitFor(() => expect(api.stopChatTurn).toHaveBeenCalled())
    await waitFor(() => expect(screen.queryByText('Stopping')).not.toBeInTheDocument())
    expect(screen.getByRole('button', { name: 'Stop generating' })).toBeInTheDocument()
  })

  it('offers Jump to latest when new content arrives while the reader is scrolled away', async () => {
    render(<Harness />)
    await waitFor(() => expect(box()).not.toBeDisabled())
    const sc = screen.getByTestId('bubble-transcript')
    Object.defineProperty(sc, 'scrollHeight', { value: 1000, configurable: true })
    Object.defineProperty(sc, 'clientHeight', { value: 500, configurable: true })
    Object.defineProperty(sc, 'scrollTop', { value: 100, writable: true, configurable: true })
    fireEvent.scroll(sc)
    await sendHello()
    fireEvent.click(await screen.findByText(/Jump to latest/))
    await waitFor(() => expect(screen.queryByText(/Jump to latest/)).not.toBeInTheDocument())
  })
})

describe('a new chat while a runtime loads its models', () => {
  it("never creates with the previous runtime's model, and cannot send until the new list arrives", async () => {
    api.getAgentRuntimeModels.mockImplementation(rt => (rt === 'codex' ? new Promise(() => {}) : Promise.resolve([{ id: 'claude-opus-5', label: 'Opus 5' }])))
    render(<Harness seed="draft" />)
    const runtime = await screen.findByLabelText('Runtime')
    await waitFor(() => expect(screen.getByLabelText('Model')).toHaveValue('claude-opus-5'))
    fireEvent.change(runtime, { target: { value: 'codex' } })
    await waitFor(() => expect(api.getAgentRuntimeModels).toHaveBeenCalledWith('codex', '/bin/codex'))
    expect(screen.queryByText('Opus 5')).not.toBeInTheDocument()
    expect(box()).toBeDisabled()
    fireEvent.change(box(), { target: { value: 'hello' } })
    fireEvent.keyDown(box(), { key: 'Enter' })
    await new Promise(r => setTimeout(r, 20))
    expect(chatAccess.create).not.toHaveBeenCalled()
  })
})
