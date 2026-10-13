import { describe, it, expect } from 'vitest'
import { normalizeAsk, normalizeAsks, asksFor, pendingCount, formatBytes, safeUrl, safeImageSrc } from './askModel.js'
import sendEmail from '../components/bubbles/__fixtures__/hil-send-email.json'
import workflow from '../components/bubbles/__fixtures__/hil-workflow.json'

describe('normalizeAsk', () => {
  it('reads a send item by the contract', () => {
    const a = normalizeAsk(sendEmail)
    expect(a).toMatchObject({ kind: 'send', id: 'hil-send-1', pending: true, role: 'dev', channel: 'email', subject: 'Release notes for v1.2' })
    expect(a.attachments).toHaveLength(3)
    expect(a.recipient.name).toBe('Sam Rivera')
  })
  it('reads a workflow item, with the org it belongs to', () => {
    const a = normalizeAsk(workflow)
    expect(a).toMatchObject({ kind: 'hil', id: 'hil-wf-1', pending: true, workflowName: 'Weekly outreach', org: 'acme' })
    expect(a.editable).toEqual({ caption: 'New drop is live' })
  })
  it('ends a send item that is no longer pending', () => {
    expect(normalizeAsk({ ...sendEmail, state: 'sent' }).pending).toBe(false)
  })
  it('drops junk', () => {
    expect(normalizeAsks([null, {}, 'x', sendEmail]).map(a => a.id)).toEqual(['hil-send-1'])
    expect(normalizeAsks({ error: 'x' })).toEqual([])
  })
})

describe('asksFor', () => {
  const asks = normalizeAsks([
    sendEmail,
    { ...sendEmail, id: 's-conv', preparedBy: {}, conversationId: 'c1' },
    { ...sendEmail, id: 's-other', preparedBy: { role: 'ghost' } },
    workflow,
    { ...workflow, id: 'w-free', node_config: {} },
  ])
  it('gives an org bubble its tagged asks and the sends its roles prepared', () => {
    expect(asksFor(asks, { org: 'acme', roleIds: ['ceo', 'dev'] }).map(a => a.id)).toEqual(['hil-send-1', 'hil-wf-1'])
    expect(asksFor(asks, { org: 'other', roleIds: [] })).toEqual([])
  })
  it('gives a coder bubble its conversation, and untagged sends', () => {
    expect(asksFor(asks, { conversationId: 'c1' }).map(a => a.id)).toEqual(['hil-send-1', 's-conv', 's-other'])
    expect(asksFor(asks, {})).toEqual([])
  })
  it('counts the pending ones', () => {
    expect(pendingCount(asks)).toBe(5)
  })
})

describe('helpers', () => {
  it('formats sizes', () => {
    expect(formatBytes(12)).toBe('12 B')
    expect(formatBytes(48213)).toBe('47 KB')
    expect(formatBytes(2411724)).toBe('2.3 MB')
    expect(formatBytes(-1)).toBe('')
  })
  it('only trusts http(s) links and image data urls', () => {
    expect(safeUrl('https://a.test/x')).toBe('https://a.test/x')
    expect(safeUrl('javascript:alert(1)')).toBe('')
    expect(safeUrl('file:///etc/passwd')).toBe('')
    expect(safeImageSrc('data:image/png;base64,AAAA')).toBe('data:image/png;base64,AAAA')
    expect(safeImageSrc('data:text/html;base64,AAAA')).toBe('')
    expect(safeImageSrc('')).toBe('')
  })
})

import { normalizeAsk as normalizeAskNested } from './askModel.js'
import { describe as describeNested, it as itNested, expect as expectNested } from 'vitest'

describeNested('normalizeAsk with `hil list` send rows', () => {
  itNested('reads the payload nested under .send and tags the chat from preparedBy', () => {
    const ask = normalizeAskNested({
      id: 'h1', kind: 'send',
      send: { id: 'h1', state: 'pending', text: 'Hi', preparedBy: { role: 'Writer', conversationId: 'c9' }, recipient: { name: 'Ana' } },
    })
    expectNested(ask).toMatchObject({ kind: 'send', pending: true, text: 'Hi', role: 'Writer', conversationId: 'c9' })
    expectNested(ask.recipient.name).toBe('Ana')
  })
})
