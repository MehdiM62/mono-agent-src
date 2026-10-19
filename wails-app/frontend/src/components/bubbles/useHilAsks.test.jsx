// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act, cleanup } from '@testing-library/react'

const { hilApi } = vi.hoisted(() => ({ hilApi: { listAsks: vi.fn(), resolve: vi.fn() } }))
vi.mock('../../services/hilApi.js', () => hilApi)

import { useHilAsks } from './useHilAsks.js'

beforeEach(() => {
  vi.clearAllMocks()
  hilApi.listAsks.mockResolvedValue([])
  hilApi.resolve.mockResolvedValue({ ok: true, state: 'sent' })
})
afterEach(cleanup)

describe('useHilAsks.resolve', () => {
  it("hands a card's always allow to the send resolve", async () => {
    const { result } = renderHook(() => useHilAsks({ conversationId: 'c1' }))
    await act(async () => { await result.current.resolve({ id: 's1', kind: 'send' }, true, { editText: 'hi', alwaysAllow: true }) })
    expect(hilApi.resolve).toHaveBeenCalledWith('s1', { approve: true, editText: 'hi', data: undefined, kind: 'send', alwaysAllow: true })
  })
})
