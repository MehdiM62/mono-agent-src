// @vitest-environment jsdom
import React, { useEffect } from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup, within } from '@testing-library/react'
import '../../i18n.js'

// The pane's header chips and settings popover: what each control reads, and
// that the only things it changes are the access level and the org mode.
const { api, chatAccess, budget, mockConfirm } = vi.hoisted(() => ({
  mockConfirm: vi.fn(),
  chatAccess: { create: vi.fn(), setAccess: vi.fn() },
  budget: { forTask: vi.fn(), list: vi.fn() },
  api: {
    getChatTurns: vi.fn(), getChatEvents: vi.fn(), startChatTurn: vi.fn(), stopChatTurn: vi.fn(), coderStatus: vi.fn(),
    coderWorkspaceList: vi.fn(), getAgentRuntimeModels: vi.fn(), getProfiles: vi.fn(), listChatConversations: vi.fn(),
    browserPermissions: vi.fn(), setChatOrgMode: vi.fn(), getActiveProfile: vi.fn(),
  },
}))
vi.mock('../../services/api.js', () => ({
  api, notify: vi.fn(), onChatEvent: () => () => {}, onOrgEvent: () => () => {}, onOrgRunStatus: () => () => {}, newOrgEventsStreamId: () => 's',
}))
vi.mock('../../lib/chatAccessApi.js', () => ({ chatAccessApi: chatAccess }))
vi.mock('../settings/aitaskBudgetApi.js', () => ({ aitaskBudgetApi: budget, money: n => `$${Number(n || 0).toFixed(2)}` }))
vi.mock('../ConfirmDialog.jsx', () => ({ confirm: (...a) => mockConfirm(...a) }))

import CoderBubbles from './CoderBubbles.jsx'
import { useCoderBubbles } from './useCoderBubbles.js'

let store
const navigate = vi.fn()
function Harness({ conv, task }) {
  store = useCoderBubbles()
  useEffect(() => { const key = store.openConversation(conv); if (task) store.openDraft({ conversationId: conv.id, task }) }, []) // eslint-disable-line react-hooks/exhaustive-deps
  return <CoderBubbles store={store} onNavigate={navigate} />
}
const overlay = () => screen.queryByTestId('bubble-overlay')
const popover = () => screen.queryByTestId('pane-settings')
const gear = () => screen.getByRole('button', { name: 'Chat settings' })
const scoped = { id: 's1', access: 'scoped', runtimeId: 'claude', model: 'opus', effort: 'high', title: 'Summarise my week' }
const coding = { id: 'k1', access: 'coding', runtimeId: 'codex', model: 'gpt-5', cwd: '/home/u/proj' }
const open = async (conv, task) => {
  render(<Harness conv={conv} task={task} />)
  await waitFor(() => expect(overlay()).toBeInTheDocument())
}
const kinds = [{ kind: 'apply', label: 'Apply', level: 'ask', isDefault: true }, { kind: 'chat', label: 'Chat', level: 'ask', isDefault: true }, { kind: 'coder', label: 'Coder', level: 'dev', isDefault: false }]

beforeEach(() => {
  vi.clearAllMocks()
  Element.prototype.scrollTo = function scrollTo() {}
  localStorage.clear()
  api.getChatTurns.mockResolvedValue({ items: [] })
  api.getChatEvents.mockResolvedValue({ items: [], hasMore: false })
  api.coderStatus.mockResolvedValue({ enabled: true, ready: true })
  api.coderWorkspaceList.mockResolvedValue([])
  api.getProfiles.mockResolvedValue([])
  api.getActiveProfile.mockResolvedValue(null)
  api.listChatConversations.mockResolvedValue({ items: [] })
  api.browserPermissions.mockResolvedValue({ kinds, catalog: [{ id: 'ask', label: 'Ask first' }, { id: 'dev', label: 'Developer' }] })
  api.setChatOrgMode.mockResolvedValue({ ok: true })
  budget.list.mockResolvedValue({ caps: [{ scope: 'global', key: '', capUsd: 5, usedUsd: 1.5 }], spentUsd: 1.5 })
  budget.forTask.mockResolvedValue({ taskId: 't1', capUsd: 2, spentUsd: 0.5, reported: true })
  chatAccess.setAccess.mockImplementation(async (id, level) => ({ conversation: { id, access: level, cwd: '/home/u/profile' }, previousAccess: 'coding', lowered: null }))
  mockConfirm.mockResolvedValue(true)
})
afterEach(cleanup)

describe('header chips', () => {
  it('shows the access level, the title and the runtime of a scoped chat', async () => {
    await open(scoped)
    expect(screen.getByTestId('chip-access')).toHaveTextContent('Access: Scoped')
    expect(within(overlay()).getByText('Summarise my week')).toBeInTheDocument()
    expect(within(overlay()).getByText(/Claude.*opus/i)).toBeInTheDocument()
    // A scoped chat starts no workers: no org stage, no Org control.
    expect(screen.queryByRole('button', { name: /^Org$/ })).toBeNull()
    expect(document.querySelector('[data-testid="stage-node"]')).toBeNull()
  })

  it('shows a coding chat with its org control and its stage', async () => {
    await open(coding)
    expect(screen.getByTestId('chip-access')).toHaveTextContent('Access: Coding')
    expect(screen.getByRole('button', { name: /Org/ })).toBeInTheDocument()
    expect(document.querySelector('[data-testid="stage-node"]')).not.toBeNull()
  })

  it('reads a bubble opened with no level from the conversation list', async () => {
    api.listChatConversations.mockResolvedValue({ items: [{ id: 'k1', mode: 'coder', access: 'workspace', orgMode: 'dynamic' }] })
    await open({ id: 'k1' })
    await waitFor(() => expect(screen.getByTestId('chip-access')).toHaveTextContent('Access: Workspace'))
    expect(store.bubbles[0].orgMode).toBe('dynamic')
  })
})

describe('settings popover', () => {
  it('opens with focus inside, lists what the chat runs on, and returns focus on close', async () => {
    await open(scoped)
    fireEvent.click(gear())
    expect(popover()).toBeInTheDocument()
    expect(gear()).toHaveAttribute('aria-expanded', 'true')
    expect(within(popover()).getByText(/opus · high/)).toBeInTheDocument()
    expect(popover().contains(document.activeElement)).toBe(true)
    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(popover()).toBeNull())
    expect(overlay()).toBeInTheDocument() // the first Esc closed the popover only
    expect(document.activeElement).toBe(gear())
  })

  it('Esc layers: popover first, then collapse, and never stops a running turn', async () => {
    api.getChatTurns.mockResolvedValue({ items: [{ id: 't1', prompt: 'work', status: 'active', ownedByThisInstance: true }] })
    await open(coding)
    await waitFor(() => expect(screen.getByText('work')).toBeInTheDocument())
    fireEvent.click(gear())
    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(popover()).toBeNull())
    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(overlay()).toBeNull())
    expect(api.stopChatTurn).not.toHaveBeenCalled()
  })

  it('shows the task cap for a task-bound chat and the global cap otherwise, with a Settings link and no raise control', async () => {
    await open(coding, { taskId: 't1', goal: 'do it', status: 'running', kind: 'generic' })
    fireEvent.click(gear())
    await waitFor(() => expect(within(screen.getByTestId('pane-cost')).getByText('$0.50 of $2.00 (this task\'s cap)')).toBeInTheDocument())
    expect(budget.forTask).toHaveBeenCalledWith('t1')
    fireEvent.click(within(screen.getByTestId('pane-cost')).getByRole('button', { name: /budgets in Settings/i }))
    expect(navigate).toHaveBeenCalledWith('settings')
  })

  it('shows the global cap for a chat with no task, and says so when there is none', async () => {
    await open(scoped)
    fireEvent.click(gear())
    await waitFor(() => expect(within(screen.getByTestId('pane-cost')).getByText('$1.50 of $5.00 (global cap)')).toBeInTheDocument())
    expect(within(popover()).queryByRole('spinbutton')).toBeNull()
    cleanup()
    budget.list.mockResolvedValue({ caps: [], spentUsd: 0 })
    await open(scoped)
    fireEvent.click(gear())
    await waitFor(() => expect(within(screen.getByTestId('pane-cost')).getByText('No spend cap set.')).toBeInTheDocument())
  })

  it('reads the browser level for the chat kind when scoped and for the coder kind otherwise, read-only', async () => {
    await open(scoped)
    fireEvent.click(gear())
    await waitFor(() => expect(within(screen.getByTestId('pane-browser')).getByText('Ask first')).toBeInTheDocument())
    cleanup()
    await open(coding)
    fireEvent.click(gear())
    await waitFor(() => expect(within(screen.getByTestId('pane-browser')).getByText('Developer')).toBeInTheDocument())
    expect(within(screen.getByTestId('pane-browser')).queryByRole('combobox')).toBeNull()
  })

  it('shows the assistant tools for a scoped chat with a link, not a toggle, and the org switch for a coding chat only', async () => {
    await open(scoped)
    fireEvent.click(gear())
    expect(screen.getByTestId('pane-tools')).toHaveTextContent('MonoAgent tools on, workflow runs on')
    expect(within(screen.getByTestId('pane-tools')).queryByRole('switch')).toBeNull()
    expect(within(popover()).queryByRole('switch')).toBeNull()
    cleanup()
    await open(coding)
    fireEvent.click(gear())
    expect(screen.queryByTestId('pane-tools')).toBeNull()
    const sw = within(popover()).getByRole('switch', { name: 'Org mode' })
    expect(sw).toHaveAttribute('aria-checked', 'false')
    fireEvent.click(sw)
    await waitFor(() => expect(api.setChatOrgMode).toHaveBeenCalledWith('k1', 'dynamic'))
    await waitFor(() => expect(sw).toHaveAttribute('aria-checked', 'true'))
    fireEvent.click(sw)
    await waitFor(() => expect(api.setChatOrgMode).toHaveBeenLastCalledWith('k1', 'solo'))
  })
})

describe('access control', () => {
  it('lowers at once, without asking, and keeps the new level', async () => {
    await open(coding)
    fireEvent.click(gear())
    fireEvent.change(within(popover()).getByLabelText('Access'), { target: { value: 'scoped' } })
    await waitFor(() => expect(chatAccess.setAccess).toHaveBeenCalledWith('k1', 'scoped'))
    expect(mockConfirm).not.toHaveBeenCalled()
    await waitFor(() => expect(store.bubbles[0].access).toBe('scoped'))
    expect(screen.getByTestId('chip-access')).toHaveTextContent('Access: Scoped')
    expect(within(popover()).getByText('Access is now Scoped.')).toBeInTheDocument()
  })

  it('names the processes a lowering could not stop', async () => {
    chatAccess.setAccess.mockResolvedValue({
      conversation: { id: 'k1', access: 'scoped' }, previousAccess: 'coding',
      lowered: { stopped: [], survivors: [{ pid: 4242, command: 'node server.js', reason: 'owned by another user' }], workersDropped: false, taskRevoked: false },
    })
    await open(coding)
    fireEvent.click(gear())
    fireEvent.change(within(popover()).getByLabelText('Access'), { target: { value: 'scoped' } })
    const alert = await screen.findByTestId('pane-survivors')
    expect(alert).toHaveAttribute('role', 'alert')
    expect(alert).toHaveTextContent('1 process could not be stopped and is still running')
    expect(alert).toHaveTextContent('4242 node server.js (owned by another user)')
  })

  it('raising asks first, stating what the level allows, and changes nothing when declined', async () => {
    mockConfirm.mockResolvedValue(false)
    await open(coding)
    fireEvent.click(gear())
    fireEvent.change(within(popover()).getByLabelText('Access'), { target: { value: 'full' } })
    await waitFor(() => expect(mockConfirm).toHaveBeenCalled())
    const [message, opts] = mockConfirm.mock.calls[0]
    expect(opts.title).toBe('Raise access to Full?')
    expect(message).toContain('Only you can raise it')
    expect(message).toContain('The same as Workspace')
    expect(opts.danger).toBe(true)
    expect(chatAccess.setAccess).not.toHaveBeenCalled()
    expect(store.bubbles[0].access).toBe('coding')
  })

  it('raises after the confirmation and shows the folder a scoped chat gets', async () => {
    await open(scoped)
    fireEvent.click(gear())
    fireEvent.change(within(popover()).getByLabelText('Access'), { target: { value: 'coding' } })
    await waitFor(() => expect(chatAccess.setAccess).toHaveBeenCalledWith('s1', 'coding'))
    await waitFor(() => expect(store.bubbles[0]).toMatchObject({ access: 'coding', cwd: '/home/u/profile' }))
  })

  it('says plainly when the CLI refuses a raise as operator-only', async () => {
    const err = new Error('operator_only: raising a conversation is the operator\'s')
    err.code = 'operator_only'
    chatAccess.setAccess.mockRejectedValue(err)
    await open(coding)
    fireEvent.click(gear())
    fireEvent.change(within(popover()).getByLabelText('Access'), { target: { value: 'full' } })
    expect(await screen.findByText(/Only you, in this window, can raise access/)).toBeInTheDocument()
    expect(store.bubbles[0].access).toBe('coding')
  })

  it('treats an unknown current level as a raise, so it asks', async () => {
    api.listChatConversations.mockResolvedValue({ items: [] })
    await open({ id: 'k2' })
    fireEvent.click(gear())
    fireEvent.change(within(popover()).getByLabelText('Access'), { target: { value: 'scoped' } })
    await waitFor(() => expect(mockConfirm).toHaveBeenCalled())
  })
})

describe('a confined application-apply chat', () => {
  const task = { taskId: 't9', goal: 'Apply to Acme', status: 'running', kind: 'application-apply' }

  it('hides the Org control and stage and the access control, and shows the confined chip', async () => {
    await open(coding, task)
    expect(screen.getByTestId('chip-confined')).toHaveTextContent('confined: browser only')
    expect(screen.queryByTestId('chip-access')).toBeNull()
    expect(screen.queryByRole('button', { name: /^Org$/ })).toBeNull()
    expect(document.querySelector('[data-testid="stage-node"]')).toBeNull()
    fireEvent.click(gear())
    expect(within(popover()).queryByLabelText('Access')).toBeNull()
    expect(screen.getByTestId('pane-access-confined')).toHaveTextContent('Confined to the browser')
    expect(within(popover()).queryByRole('switch')).toBeNull()
  })

  it('reads the apply browser level, not coder, with its own Settings link', async () => {
    api.browserPermissions.mockResolvedValue({
      kinds: [...kinds.filter(k => k.kind !== 'apply'), { kind: 'apply', label: 'Apply', level: 'ask', isDefault: true }], catalog: [{ id: 'ask', label: 'Ask first' }, { id: 'dev', label: 'Developer' }],
    })
    await open(coding, task)
    fireEvent.click(gear())
    await waitFor(() => expect(within(screen.getByTestId('pane-browser')).getByText('Ask first')).toBeInTheDocument())
    expect(within(screen.getByTestId('pane-browser')).queryByText('Developer')).toBeNull()
    expect(within(screen.getByTestId('pane-browser')).getByRole('button', { name: 'Browser access (apply) in Settings' })).toBeInTheDocument()
  })

  it('is confined the moment it opens from the button, with no stored state', async () => {
    await open({ id: 'k7' }, { taskId: 't7', kind: 'application-apply', goal: 'g', status: 'running' })
    expect(await screen.findByTestId('chip-confined')).toBeInTheDocument()
    expect(store.bubbles[0].taskKind).toBe('application-apply')
  })

  it('stays confined after a restart, from the stored task kind', async () => {
    await open(coding, task)
    const saved = localStorage.getItem('monoagent:coderBubbles:v1')
    cleanup()
    localStorage.setItem('monoagent:coderBubbles:v1', saved)
    render(<Harness conv={{ id: 'other' }} />)
    // The restored bubble is the first one; open it.
    await waitFor(() => expect(document.querySelector('[data-bubble="k1"]')).toBeInTheDocument())
    fireEvent.click(document.querySelector('[data-bubble="k1"]'))
    await waitFor(() => expect(screen.getByTestId('chip-confined')).toBeInTheDocument())
  })
})
