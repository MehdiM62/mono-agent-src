import { describe, it, expect, vi, beforeEach } from 'vitest'

const { api } = vi.hoisted(() => ({ api: { hilSendResolve: vi.fn(), hilResolve: vi.fn(), hilList: vi.fn() } }))
vi.mock('./api.js', () => ({ api }))
vi.mock('../lib/commsStatus.js', () => ({ emitCommsChanged: vi.fn() }))

import { resolve } from './hilApi.js'

beforeEach(() => {
  vi.clearAllMocks()
  api.hilSendResolve.mockResolvedValue({ state: 'sent' })
})

describe('hilApi.resolve for a send', () => {
  it('passes always allow with an approval', async () => {
    expect(await resolve('s1', { approve: true, kind: 'send', editText: 'hi', alwaysAllow: true })).toEqual({ ok: true, state: 'sent' })
    expect(api.hilSendResolve).toHaveBeenCalledWith('s1', true, { editText: 'hi', alwaysAllow: true })
  })

  it('never with a rejection, and not unless asked', async () => {
    api.hilSendResolve.mockResolvedValue({ state: 'rejected' })
    await resolve('s1', { approve: false, kind: 'send', alwaysAllow: true })
    expect(api.hilSendResolve).toHaveBeenLastCalledWith('s1', false, { editText: '' })
    await resolve('s2', { approve: true, kind: 'send' })
    expect(api.hilSendResolve).toHaveBeenLastCalledWith('s2', true, { editText: '' })
  })
})
