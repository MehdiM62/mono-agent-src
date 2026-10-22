import { describe, it, expect } from 'vitest'
import { bubbleKeyOf, liveStatus, relativeTime, toFilter, orgRunTarget, orgRunLabel, mergePage, deleteOutcome, chatIdsOnly } from './historyModel.js'

describe('historyModel', () => {
  it('keys chats by id and orgs by name', () => {
    expect(bubbleKeyOf({ kind: 'chat', id: 'c1' })).toBe('c1')
    expect(bubbleKeyOf({ kind: 'org', id: 'r1', orgName: 'acme' })).toBe('org:acme')
  })
  it('lets a live running/needs summary override the stored status only', () => {
    const row = { status: 'done' }
    expect(liveStatus(row, null).status).toBe('done')
    expect(liveStatus(row, { status: 'working', unread: 2 })).toMatchObject({ status: 'running', unread: 2 })
    expect(liveStatus(row, { status: 'needs', needs: 1 })).toMatchObject({ status: 'needs', needs: 1 })
    expect(liveStatus({ status: 'error' }, { status: 'idle' }).status).toBe('error')
  })
  it('reads relative times', () => {
    const now = Date.parse('2026-10-22T12:00:00Z')
    expect(relativeTime('2026-10-22T11:59:50Z', now)).toBe('just now')
    expect(relativeTime('2026-10-22T11:55:00Z', now)).toBe('5m ago')
    expect(relativeTime('2026-10-22T09:00:00Z', now)).toBe('3h ago')
    expect(relativeTime('2026-10-20T12:00:00Z', now)).toBe('2d ago')
    expect(relativeTime('2026-09-01T12:00:00Z', now)).toBe('2026-09-01')
    expect(relativeTime('', now)).toBe('')
  })
  it('builds the filter; linked asks the CLI for any application', () => {
    expect(toFilter({ kind: 'chat', query: '  x ' })).toEqual({ kind: 'chat', status: '', application: '', query: 'x' })
    expect(toFilter({ linked: true }).application).toBe('any')
  })
  it('opens only the latest run of an org as a bubble', () => {
    expect(orgRunTarget({ runId: 'r2', status: 'done' }, { run: 'r2' })).toBe('bubble')
    expect(orgRunTarget({ runId: 'r1', status: 'done' }, { run: 'r2' })).toBe('orgs')
    expect(orgRunTarget({ runId: '', status: 'running' }, null)).toBe('bubble')
    expect(orgRunTarget({ runId: '', status: 'done' }, { run: '' })).toBe('orgs')
  })
  it('labels a run', () => {
    expect(orgRunLabel({ runId: 'abcdef123456', updatedAt: '2026-10-20T10:00:00Z' })).toBe('run abcdef12 \u00b7 2026-10-20')
    expect(orgRunLabel({ updatedAt: '2026-10-20T10:00:00Z' })).toBe('2026-10-20')
  })
  it('merges pages without duplicates', () => {
    expect(mergePage([{ id: 'a' }], [{ id: 'a' }, { id: 'b' }]).map(r => r.id)).toEqual(['a', 'b'])
  })
  it('splits a delete result and never lets org runs through', () => {
    expect(deleteOutcome({ deleted: ['a'], active: ['b'], failed: { c: 'boom' } })).toEqual({ deleted: ['a'], active: ['b'], failed: [{ id: 'c', message: 'boom' }] })
    expect(chatIdsOnly([{ kind: 'chat', id: 'a' }, { kind: 'org', id: 'o' }])).toEqual(['a'])
  })
})
