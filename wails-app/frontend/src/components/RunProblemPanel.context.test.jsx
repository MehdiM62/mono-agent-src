import { describe, it, expect } from 'vitest'
import { failedRunContext } from './RunProblemPanel.jsx'

const bytes = (s) => new TextEncoder().encode(s).length

describe('failedRunContext', () => {
  it('carries the ids, the failing node ids and their errors', () => {
    const out = failedRunContext({ error: 'run died', failed: [{ id: 'n1', name: 'HTTP', error: 'boom' }] }, 'wf-1', 'ex-1')
    expect(out).toBe('workflow: wf-1\nexecution: ex-1\nrun error: run died\nnode n1 (HTTP): boom')
  })

  it('cuts each error at 500 chars', () => {
    const out = failedRunContext({ error: '', failed: [{ id: 'n1', name: 'A', error: 'x'.repeat(2000) }] }, 'w', 'e')
    const errLine = out.split('\n').find(l => l.startsWith('node n1'))
    expect(errLine.endsWith('…')).toBe(true)
    expect(errLine.length).toBeLessThan(540)
  })

  it('stays within 4 KB however many nodes failed', () => {
    const failed = Array.from({ length: 100 }, (_, i) => ({ id: `n${i}`, name: `N${i}`, error: 'é'.repeat(500) }))
    const out = failedRunContext({ error: 'e', failed }, 'w', 'e')
    expect(bytes(out)).toBeLessThanOrEqual(4096)
    expect(out.startsWith('workflow: w\nexecution: e')).toBe(true)
  })
})
