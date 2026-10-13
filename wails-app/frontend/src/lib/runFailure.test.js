import { describe, it, expect } from 'vitest'
import { isInactiveRunError, runProblem } from './runFailure.js'

const nodes = [
  { id: 't', label: 'Manual Trigger', subtype: 'trigger.manual' },
  { id: 'g', label: 'Generate Image', subtype: 'gemini.generate_image' },
]

describe('runProblem', () => {
  it('is null for runs with nothing to explain', () => {
    expect(runProblem({ status: 'SUCCESS', nodes: [] }, nodes)).toBeNull()
    expect(runProblem({ status: 'RUNNING' }, nodes)).toBeNull()
    expect(runProblem(null, nodes)).toBeNull()
  })

  it('points an engine-level unknown node type at the node of that type', () => {
    const p = runProblem({
      status: 'FAILED', error: 'workflow: unknown node type: gemini.generate_image',
      nodes: [{ node_id: 't', node_name: 'Manual Trigger', status: 'SUCCESS' }],
    }, nodes)
    expect(p.error).toBe('workflow: unknown node type: gemini.generate_image')
    expect(p.failed).toEqual([{ id: 'g', name: 'Generate Image', error: p.error, inferred: true }])
  })

  it('lists node-level failures with their own errors', () => {
    const p = runProblem({
      status: 'SUCCESS_WITH_ERRORS', error: '',
      nodes: [
        { node_id: 't', node_name: 'Manual Trigger', status: 'SUCCESS' },
        { node_id: 'g', node_name: 'Generate Image', status: 'FAILED', error_message: 'keywords is required' },
      ],
    }, nodes)
    expect(p.status).toBe('SUCCESS_WITH_ERRORS')
    expect(p.failed).toEqual([{ id: 'g', name: 'Generate Image', error: 'keywords is required' }])
  })

  it('still says something when a failed run carries no message', () => {
    expect(runProblem({ status: 'FAILED', nodes: [] }, nodes).error).toMatch(/without an error message/)
  })
})

describe('isInactiveRunError', () => {
  it('recognises the inactive refusals', () => {
    expect(isInactiveRunError('workflow "Gemini" is inactive — activate it first')).toBe(true)
    expect(isInactiveRunError('trigger workflow: workflow: workflow is not active')).toBe(true)
    expect(isInactiveRunError('workflow abc is already running')).toBe(false)
    expect(isInactiveRunError(undefined)).toBe(false)
  })
})
