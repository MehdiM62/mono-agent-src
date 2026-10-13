import { describe, it, expect } from 'vitest'
import { messageStatus, matchesStatusFilter, statusTimeline, commsByPerson, isCompany, isOwnAccount, askHilRef, askPersonId } from './commsStatus.js'

describe('commsStatus', () => {
  it('old rows are sent, inbound have no status', () => {
    expect(messageStatus({ direction: 'outbound' })).toBe('sent')
    expect(messageStatus({ direction: 'outbound', status: 'pending_approval' })).toBe('pending_approval')
    expect(messageStatus({ direction: 'inbound', status: 'sent' })).toBeNull()
  })
  it('filters group sending with approved and expired with failed', () => {
    expect(matchesStatusFilter({ status: 'sending' }, 'approved')).toBe(true)
    expect(matchesStatusFilter({ status: 'expired' }, 'failed')).toBe(true)
    expect(matchesStatusFilter({ status: 'sent' }, 'failed')).toBe(false)
    expect(matchesStatusFilter({ status: 'sent' }, 'all')).toBe(true)
  })
  it('reads a timeline from metadata (object or JSON), else the current status', () => {
    const h = [{ status: 'pending_approval', at: '2026-10-01T10:00:00Z' }, { status: 'sent', at: '2026-10-01T11:00:00Z' }]
    expect(statusTimeline({ status: 'sent', metadata: { status_history: h } })).toHaveLength(2)
    expect(statusTimeline({ status: 'sent', metadata: JSON.stringify({ status_history: h }) })[1].status).toBe('sent')
    expect(statusTimeline({ status: 'failed', sent_at: 'x', metadata: '{bad' })).toEqual([{ status: 'failed', at: 'x' }])
    expect(statusTimeline({ direction: 'inbound' })).toEqual([])
  })
  it('counts communications per person incl. pending', () => {
    const c = commsByPerson([{ person_id: 'a', status: 'pending_approval' }, { person_id: 'a', status: 'sent' }, { person_id: 'b' }])
    expect(c.a).toEqual({ total: 2, pending: 1 })
    expect(c.b).toEqual({ total: 1, pending: 0 })
  })
  it('kind and ask accessors', () => {
    expect(isCompany({ kind: 'company' })).toBe(true)
    expect(isCompany({ kind: 'person' })).toBe(false)
    expect(isOwnAccount({ is_own_account: true })).toBe(true)
    expect(askHilRef({ kind: 'send', id: 'h1' })).toBe('h1')
    expect(askHilRef({ kind: 'hil', id: 'h2' })).toBe('')
    expect(askPersonId({ recipient: { personId: 'p' } })).toBe('p')
  })
})
