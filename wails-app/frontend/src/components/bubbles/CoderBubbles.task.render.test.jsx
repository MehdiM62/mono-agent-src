// @vitest-environment jsdom
import React, { useEffect } from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'
import '../../i18n.js'

const { api, aitaskApi } = vi.hoisted(() => ({
  api: {
    getChatTurns: vi.fn(), getChatEvents: vi.fn(), startChatTurn: vi.fn(), stopChatTurn: vi.fn(), coderStatus: vi.fn(),
    coderWorkspaceList: vi.fn(), coderWorkspaceDefault: vi.fn(), createCoderConversation: vi.fn(),
    getAgentRuntimeModels: vi.fn(), pickCoderFolder: vi.fn(), openPathWithOS: vi.fn(), getProfiles: vi.fn(),
  },
  aitaskApi: { status: vi.fn(), submit: vi.fn(), get: vi.fn(), cancel: vi.fn() },
}))
vi.mock('../../services/api.js', () => ({
  api, notify: vi.fn(), onChatEvent: () => () => {}, onOrgEvent: () => () => {}, onOrgRunStatus: () => () => {}, newOrgEventsStreamId: () => 's',
}))
vi.mock('../SendToCoderApi.js', async (orig) => ({ ...(await orig()), aitaskApi }))
vi.mock('../ConfirmDialog.jsx', () => ({ confirm: vi.fn() }))

import CoderBubbles from './CoderBubbles.jsx'
import { useCoderBubbles } from './useCoderBubbles.js'

const task = { taskId: 't1', goal: 'Fix the failing run', status: 'running', origin: { page: 'noderunner', label: 'Workflows' } }

function Harness({ seed, onNavigate }) {
  const store = useCoderBubbles()
  useEffect(() => { store.openDraft(seed) }, []) // eslint-disable-line react-hooks/exhaustive-deps
  return <CoderBubbles store={store} onNavigate={onNavigate} />
}

beforeEach(() => {
  vi.clearAllMocks()
  Element.prototype.scrollTo = function scrollTo() {}
  localStorage.clear()
  api.getChatTurns.mockResolvedValue({ items: [] })
  api.getChatEvents.mockResolvedValue({ items: [], hasMore: false })
  api.coderStatus.mockResolvedValue({ enabled: true, ready: true })
  api.coderWorkspaceList.mockResolvedValue([])
  api.getAgentRuntimeModels.mockResolvedValue([])
  api.getProfiles.mockResolvedValue([])
})
afterEach(cleanup)

describe('a task bubble', () => {
  it('shows the task header over the chat, and Open in <tab> navigates back', async () => {
    const onNavigate = vi.fn()
    render(<Harness seed={{ conversationId: 'c1', task }} onNavigate={onNavigate} />)
    const header = await screen.findByTestId('task-header')
    expect(header).toHaveTextContent('Fix the failing run')
    fireEvent.click(screen.getByRole('button', { name: 'Open in Workflows' }))
    await waitFor(() => expect(onNavigate).toHaveBeenCalledWith('noderunner'))
  })

  it('a plain chat has no task header', async () => {
    render(<Harness seed={{ conversationId: 'c2' }} onNavigate={() => {}} />)
    await screen.findByTestId('bubble-overlay')
    expect(screen.queryByTestId('task-header')).toBeNull()
  })
})
