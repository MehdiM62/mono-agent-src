import { describe, it, expect, vi, beforeEach } from 'vitest'
import * as WailsApp from '../../wailsjs/go/main/App'
import { fetchApplicationAssets, normalizeApplicationAssets } from './applicationAssets.js'
import { mockApplicationAssets } from './fixtures.js'

describe('normalizeApplicationAssets', () => {
  it('returns safe empty structure for null/undefined/non-object input', () => {
    const res = normalizeApplicationAssets(null)
    expect(res).toEqual({
      posting: null,
      documents: [],
      fill: null,
      people: [],
      messages: [],
      tasks: [],
      task: null,
      links: [],
    })
  })

  it('unwraps nested assets object if provided', () => {
    const res = normalizeApplicationAssets({ assets: mockApplicationAssets })
    expect(res.posting.url).toBe(mockApplicationAssets.posting.url)
    expect(res.documents).toHaveLength(mockApplicationAssets.documents.length)
  })

  it('normalizes camelCase and snake_case properties', () => {
    const raw = {
      posting: { captured_text: 'hello', captured_at: '2026-10-22' },
      documents: [{ id: 'd1', size_bytes: 1234, created_at: '2026-10-22' }],
      fill: { updated_at: '2026-10-22', fields: [{ label: 'Name', value: 42, generated: true }] },
      task: { task_id: 't-1', conversation_id: 'c-1', status: 'running' },
    }
    const res = normalizeApplicationAssets(raw)
    expect(res.posting.capturedText).toBe('hello')
    expect(res.documents[0].sizeBytes).toBe(1234)
    expect(res.fill.fields[0].value).toBe('42')
    expect(res.fill.fields[0].generated).toBe(true)
    expect(res.task.taskId).toBe('t-1')
    expect(res.task.conversationId).toBe('c-1')
  })
})

describe('fetchApplicationAssets', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('revert-proof: returns null when WailsApp.GetApplicationAssets is not a function', async () => {
    // Simulate missing Go binding
    const orig = WailsApp.GetApplicationAssets
    delete WailsApp.GetApplicationAssets

    try {
      const res = await fetchApplicationAssets('app-123')
      expect(res).toBeNull()
    } finally {
      WailsApp.GetApplicationAssets = orig
    }
  })

  it('returns normalized assets when GetApplicationAssets succeeds', async () => {
    WailsApp.GetApplicationAssets = vi.fn().mockResolvedValue(mockApplicationAssets)
    const res = await fetchApplicationAssets('app-test-1')
    expect(res).toBeDefined()
    expect(res.posting.url).toBe(mockApplicationAssets.posting.url)
    expect(res.task.taskId).toBe('aitask-app-apply-992')
  })

  it('returns null safely without throwing when GetApplicationAssets rejects', async () => {
    WailsApp.GetApplicationAssets = vi.fn().mockRejectedValue(new Error('backend error'))
    const res = await fetchApplicationAssets('app-test-1')
    expect(res).toBeNull()
  })
})
