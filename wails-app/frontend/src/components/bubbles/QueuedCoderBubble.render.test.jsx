// @vitest-environment jsdom
import React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup, act, within } from '@testing-library/react'
import '../../i18n.js'

const { api, listeners, mockNotify } = vi.hoisted(() => ({
  listeners: { queue: new Set() },
  mockNotify: vi.fn(),
  api: {
    getChatTurns: vi.fn(),
    getChatEvents: vi.fn(),
    startChatTurn: vi.fn(),
    stopChatTurn: vi.fn(),
    coderStatus: vi.fn(),
    coderWorkspaceList: vi.fn(),
    coderWorkspaceDefault: vi.fn(),
    createCoderConversation: vi.fn(),
    getAgentRuntimeModels: vi.fn(),
    pickCoderFolder: vi.fn(),
    openPathWithOS: vi.fn(),
    coderQueueList: vi.fn(),
    coderQueueStart: vi.fn(),
    coderQueueDismiss: vi.fn(),
    coderQueueImage: vi.fn(),
    onCoderQueue: vi.fn(),
  },
}))
vi.mock('../../services/api.js', () => ({
  api,
  notify: (...a) => mockNotify(...a),
  onChatEvent: () => () => {},
  onOrgEvent: () => () => {},
  onOrgRunStatus: () => () => {},
  newOrgEventsStreamId: () => 'stream',
}))
vi.mock('../ConfirmDialog.jsx', () => ({ confirm: vi.fn() }))

import CoderBubbles from './CoderBubbles.jsx'
import { useCoderBubbles } from './useCoderBubbles.js'

let store
function Harness() {
  store = useCoderBubbles()
  return <CoderBubbles store={store} onNavigate={() => {}} />
}

const PNG = 'data:image/png;base64,iVBORw0KGgo='
const item = (over = {}) => ({
  id: '20261015-120000-abcdef', kind: 'copy-style', status: 'queued', origin: 'http://localhost:3000', path: '/pricing',
  title: 'Pricing — Acme', note: 'make our CTA look like this', cwd: '/home/u/code/acme-web', attachments: ['styles/element.png', 'page.png'],
  ...over,
})
const started = {
  conversationId: 'c-q1', turnId: '7a1c3f9e-2b4d-4e8f-9a0b-1c2d3e4f5a6b', prompt: 'Recreate an element…\n',
  attachments: ['/home/u/code/acme-web/.monoagent/context/20261015-120000-abcdef/page.png'],
  cwd: '/home/u/code/acme-web', runtime: 'claude', model: '',
}

beforeEach(() => {
  vi.clearAllMocks()
  Element.prototype.scrollTo = function scrollTo() {}
  listeners.queue.clear()
  localStorage.clear()
  api.getChatTurns.mockResolvedValue({ items: [] })
  api.getChatEvents.mockResolvedValue({ items: [], hasMore: false })
  api.coderStatus.mockResolvedValue({ enabled: true, ready: true, workspaceRoot: '/home/u/monoagent-coder' })
  api.coderWorkspaceList.mockResolvedValue([])
  api.getAgentRuntimeModels.mockResolvedValue([])
  api.startChatTurn.mockResolvedValue({ ok: true })
  api.coderQueueList.mockResolvedValue({ items: [item()] })
  api.coderQueueImage.mockResolvedValue({ name: 'page.png', dataUrl: PNG })
  api.coderQueueStart.mockResolvedValue(started)
  api.coderQueueDismiss.mockResolvedValue({ dismissed: true })
  api.onCoderQueue.mockImplementation(cb => { listeners.queue.add(cb); return () => listeners.queue.delete(cb) })
})
afterEach(cleanup)

const card = () => screen.queryByTestId('queued-coder')

describe('queued coder chats', () => {
  it('shows the kind, page, screenshot, note and target folder', async () => {
    render(<Harness />)
    await waitFor(() => expect(card()).toBeInTheDocument())
    const c = within(card())
    expect(c.getByText('Copy style')).toBeInTheDocument()
    expect(c.getByText('Pricing — Acme')).toBeInTheDocument()
    expect(c.getByText('localhost:3000/pricing')).toBeInTheDocument()
    expect(c.getByText(/make our CTA look like this/)).toBeInTheDocument()
    expect(c.getByTestId('queued-folder')).toHaveTextContent('acme-web')
    await waitFor(() => expect(c.getByRole('img')).toHaveAttribute('src', PNG))
    expect(api.coderQueueImage).toHaveBeenCalledWith('20261015-120000-abcdef')
  })

  it('Start creates the conversation, runs the first turn with the attachments and opens the bubble', async () => {
    render(<Harness />)
    await waitFor(() => expect(card()).toBeInTheDocument())
    fireEvent.click(within(card()).getByRole('button', { name: /start/i }))
    await waitFor(() => expect(screen.queryByTestId('bubble-overlay')).toBeInTheDocument())
    expect(api.coderQueueStart).toHaveBeenCalledWith('20261015-120000-abcdef', '')
    expect(api.startChatTurn).toHaveBeenCalledWith('c-q1', started.turnId, started.prompt, false, false, started.attachments)
    expect(store.bubbles.map(b => b.conversationId)).toEqual(['c-q1'])
    expect(store.bubbles[0].cwd).toBe('/home/u/code/acme-web')
    expect(card()).toBeNull()
  })

  it('Start in a folder picked on the card', async () => {
    api.pickCoderFolder.mockResolvedValue('/home/u/code/other')
    render(<Harness />)
    await waitFor(() => expect(card()).toBeInTheDocument())
    fireEvent.click(within(card()).getByRole('button', { name: 'Change' }))
    await waitFor(() => expect(within(card()).getByTestId('queued-folder')).toHaveTextContent('other'))
    fireEvent.click(within(card()).getByRole('button', { name: /start/i }))
    await waitFor(() => expect(api.coderQueueStart).toHaveBeenCalledWith('20261015-120000-abcdef', '/home/u/code/other'))
  })

  it('a refused Start stays on the card with the reason', async () => {
    api.coderQueueStart.mockRejectedValue(new Error('coder mode is disabled'))
    render(<Harness />)
    await waitFor(() => expect(card()).toBeInTheDocument())
    fireEvent.click(within(card()).getByRole('button', { name: /start/i }))
    await waitFor(() => expect(within(card()).getByRole('alert')).toHaveTextContent('coder mode is disabled'))
    expect(api.startChatTurn).not.toHaveBeenCalled()
    expect(store.bubbles).toEqual([])
  })

  it('a turn that will not start opens the chat with the prompt as its draft', async () => {
    api.startChatTurn.mockResolvedValue({ ok: false, status: 'busy' })
    render(<Harness />)
    await waitFor(() => expect(card()).toBeInTheDocument())
    fireEvent.click(within(card()).getByRole('button', { name: /start/i }))
    await waitFor(() => expect(screen.queryByTestId('bubble-overlay')).toBeInTheDocument())
    expect(store.getView('c-q1').draft).toBe(started.prompt)
    expect(mockNotify).toHaveBeenCalled()
  })

  it('Dismiss drops it, and a coder:queue event lists again', async () => {
    render(<Harness />)
    await waitFor(() => expect(card()).toBeInTheDocument())
    fireEvent.click(within(card()).getByRole('button', { name: 'Dismiss' }))
    await waitFor(() => expect(card()).toBeNull())
    expect(api.coderQueueDismiss).toHaveBeenCalledWith('20261015-120000-abcdef')

    api.coderQueueList.mockResolvedValue({ items: [item({ id: '20261015-130000-123456', kind: 'bug-report', title: 'Checkout', note: '' })] })
    act(() => { for (const cb of [...listeners.queue]) cb({}) })
    await waitFor(() => expect(within(card()).getByText('Bug report')).toBeInTheDocument())
  })

  it('shows two at a time and the rest behind +N more', async () => {
    api.coderQueueList.mockResolvedValue({ items: [1, 2, 3, 4].map(n => item({ id: `20261015-12000${n}-abcdef`, title: `Page ${n}` })) })
    render(<Harness />)
    await waitFor(() => expect(screen.getAllByTestId('queued-coder')).toHaveLength(2))
    fireEvent.click(screen.getByRole('button', { name: '+2 more queued' }))
    expect(screen.getAllByTestId('queued-coder')).toHaveLength(4)
  })
})
