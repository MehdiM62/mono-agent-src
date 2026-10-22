// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'

const { api } = vi.hoisted(() => ({
  api: { getChatTurns: vi.fn(), getOrgDesign: vi.fn(), getOrgLogs: vi.fn() },
}))
vi.mock('../../services/api.js', () => ({
  api,
  onChatEvent: () => () => {},
  onOrgEvent: () => () => {},
}))
vi.mock('../../lib/orgEventStreams.js', () => ({ acquireOrgEvents: () => () => {} }))

import { useCoderBubbles } from './useCoderBubbles.js'

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  api.getChatTurns.mockResolvedValue({ items: [] })
})

describe('openDraft', () => {
  it('with no argument is a blank new chat that expands', () => {
    const { result } = renderHook(() => useCoderBubbles())
    let key
    act(() => { key = result.current.openDraft() })
    expect(result.current.bubbles).toHaveLength(1)
    expect(result.current.bubbles[0]).toMatchObject({ key, conversationId: '' })
    expect(result.current.expandedKey).toBe(key)
    expect(result.current.getView(key)).toEqual({})
  })

  it('treats a click event as no argument', () => {
    const { result } = renderHook(() => useCoderBubbles())
    let key
    act(() => { key = result.current.openDraft({ nativeEvent: {}, target: {}, type: 'click' }) })
    expect(result.current.getView(key)).toEqual({})
  })

  it('seeds the draft, setup and links of a new chat', () => {
    const { result } = renderHook(() => useCoderBubbles())
    const setup = { workspace: { kind: 'root' }, runtime: 'claude', model: '', effort: '' }
    const links = [{ kind: 'application', refId: 'a1' }]
    let key
    act(() => { key = result.current.openDraft({ prompt: 'Why did this fail?', setup, links }) })
    expect(result.current.getView(key)).toEqual({ draft: 'Why did this fail?', setup, links })
    expect(result.current.bubbles[0].conversationId).toBe('')
  })

  it('with a conversationId opens that conversation, reusing its bubble, and carries the task', async () => {
    const { result } = renderHook(() => useCoderBubbles())
    const task = { taskId: 't1', goal: 'g', status: 'running' }
    let key
    act(() => { key = result.current.openDraft({ conversationId: 'c1', task }) })
    expect(result.current.bubbles).toHaveLength(1)
    expect(result.current.bubbles[0]).toMatchObject({ key: 'c1', conversationId: 'c1' })
    expect(result.current.expandedKey).toBe('c1')
    expect(result.current.getView(key).task).toEqual(task)
    await waitFor(() => expect(api.getChatTurns).toHaveBeenCalledWith('c1', '', 1))
    act(() => { result.current.collapse() })
    act(() => { result.current.openDraft({ conversationId: 'c1' }) })
    expect(result.current.bubbles).toHaveLength(1)
    expect(result.current.expandedKey).toBe('c1')
  })
})
