// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, waitFor, cleanup } from '@testing-library/react'

vi.mock('../../services/api.js', () => ({
  api: {
    getSummary: vi.fn(() => Promise.resolve({ no_profile: true })),
    getExtensionStatus: vi.fn(() => Promise.resolve(null)),
    getOrgSummary: vi.fn(() => Promise.resolve(null)),
    listWorkflows: vi.fn(() => Promise.resolve([])),
    getRecentExecutions: vi.fn(() => Promise.resolve([])),
  },
  subscribeEvent: vi.fn(() => () => {}),
}))
vi.mock('../../lib/usePageVisible.js', () => ({
  usePageVisibleRef: () => ({ current: true }), useVisibleCatchUp: () => {},
}))
import { useDashboardData } from './useDashboardData.js'

afterEach(cleanup)

describe('useDashboardData with no profile', () => {
  it('reports noProfile and not a failed summary', async () => {
    const { result } = renderHook(() => useDashboardData())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.noProfile).toBe(true)
    expect(result.current.summaryFailed).toBe(false)
    expect(result.current.summary).toBeNull()
  })
})
