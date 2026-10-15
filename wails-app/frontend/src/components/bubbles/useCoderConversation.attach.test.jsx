// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act, waitFor, cleanup } from '@testing-library/react'
import '../../i18n.js'

const { api } = vi.hoisted(() => ({
  api: { getChatTurns: vi.fn(), getChatEvents: vi.fn(), startChatTurn: vi.fn(), stopChatTurn: vi.fn(), saveChatAttachment: vi.fn() },
}))
vi.mock('../../services/api.js', () => ({ api, notify: vi.fn(), onChatEvent: () => () => {} }))

import { useCoderConversation } from './useCoderConversation.js'

beforeEach(() => {
  api.startChatTurn.mockResolvedValue({ ok: true, status: 'active' })
  api.getChatEvents.mockResolvedValue({ items: [], hasMore: false })
  api.saveChatAttachment.mockImplementation((conv, name) => Promise.resolve(`/spool/${conv}/x/${name}`))
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

describe('useCoderConversation attachments', () => {
  it('saves each attachment, starts the turn with their paths and shows them on the message', async () => {
    const { result } = renderHook(() => useCoderConversation({ conversationId: '', create: () => Promise.resolve({ id: 'c9' }) }))
    const atts = [
      { id: 'a1', name: 'shot.png', mime: 'image/png', size: 3, dataUrl: 'data:image/png;base64,AQID', base64: 'AQID' },
      { id: 'a2', name: 'notes.txt', mime: 'text/plain', size: 5, dataUrl: 'data:text/plain;base64,aGVsbG8=', base64: 'aGVsbG8=' },
    ]
    let ok
    await act(async () => { ok = await result.current.send('what is wrong?', atts) })
    expect(ok).toBe(true)
    expect(api.saveChatAttachment.mock.calls).toEqual([['c9', 'shot.png', 'AQID'], ['c9', 'notes.txt', 'aGVsbG8=']])
    expect(api.startChatTurn).toHaveBeenCalledWith('c9', expect.any(String), 'what is wrong?', false, false, ['/spool/c9/x/shot.png', '/spool/c9/x/notes.txt'])
    expect(result.current.messages[0]).toEqual({
      role: 'user', content: 'what is wrong?',
      attachments: [
        { name: 'shot.png', mime: 'image/png', size: 3, dataUrl: 'data:image/png;base64,AQID' },
        { name: 'notes.txt', mime: 'text/plain', size: 5, dataUrl: 'data:text/plain;base64,aGVsbG8=' },
      ],
    })
  })

  it('reports a failed save and starts no turn', async () => {
    api.saveChatAttachment.mockRejectedValue('big.bin is over 20 MB')
    const { result } = renderHook(() => useCoderConversation({ conversationId: '', create: () => Promise.resolve({ id: 'c9' }) }))
    let ok
    await act(async () => { ok = await result.current.send('hi', [{ id: 'a', name: 'big.bin', mime: '', size: 1, base64: 'AA==' }]) })
    expect(ok).toBe(false)
    expect(api.startChatTurn).not.toHaveBeenCalled()
    expect(result.current.messages.at(-1)).toMatchObject({ role: 'error', content: 'big.bin is over 20 MB' })
  })

  it('puts a loaded turn\'s attachments back on its message', async () => {
    const att = { name: 'shot.png', path: '/w/.monoagent/attachments/t1/shot.png', mime: 'image/png', size: 3 }
    api.getChatTurns.mockResolvedValue({ items: [{ id: 't1', prompt: 'look', status: 'completed' }] })
    api.getChatEvents.mockResolvedValue({ items: [
      { conversationId: 'c1', turnId: 't1', seq: 1, at: '2026-10-15T10:00:00.000Z', type: 'turn.started', payload: { backend: 'agent', text: 'look', attachments: [att] } },
      { conversationId: 'c1', turnId: 't1', seq: 2, at: '2026-10-15T10:00:01.000Z', type: 'turn.finished', payload: { status: 'completed' } },
    ], hasMore: false })
    const { result } = renderHook(() => useCoderConversation({ conversationId: 'c1', create: vi.fn() }))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.messages[0]).toEqual({ role: 'user', content: 'look', attachments: [att] })
  })
})
