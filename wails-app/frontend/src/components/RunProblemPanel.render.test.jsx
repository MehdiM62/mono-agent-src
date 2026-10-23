// @vitest-environment jsdom
import React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'

const { aitaskApi } = vi.hoisted(() => ({
  aitaskApi: { status: vi.fn(), submit: vi.fn(), startFill: vi.fn(), get: vi.fn(), cancel: vi.fn() },
}))
vi.mock('./SendToCoderApi.js', async (orig) => ({ ...(await orig()), aitaskApi }))

import RunProblemPanel, { failedRunGoal } from './RunProblemPanel.jsx'
import { CoderBubbleContext } from './bubbles/CoderBubbleContext.jsx'

const problem = { status: 'FAILED', error: 'run died', failed: [{ id: 'n1', name: 'HTTP', error: 'boom 500' }] }
const openDraft = vi.fn()
const wrap = (ui) => <CoderBubbleContext.Provider value={{ openDraft, navigate: vi.fn() }}>{ui}</CoderBubbleContext.Provider>
const goalInfo = { workflowId: 'wf-1', workflowName: 'Daily fetch', executionId: 'ex-9' }

beforeEach(() => { vi.clearAllMocks() })
afterEach(cleanup)

describe('failedRunGoal', () => {
  it('names the workflow and the run, and says not to run it again unasked', () => {
    const g = failedRunGoal(goalInfo)
    expect(g).toContain('run ex-9')
    expect(g).toContain('"Daily fetch" (wf-1)')
    expect(g).toMatch(/Do not run or activate the workflow again unless I ask/)
  })
  it('copes with ids that are not known yet', () => {
    expect(failedRunGoal({})).toContain('the last run of a workflow')
  })
})

describe('RunProblemPanel Fix with coder', () => {
  it('submits a failed-run-fix task with the failure as selection text', async () => {
    aitaskApi.status.mockResolvedValue({ enabled: true })
    aitaskApi.submit.mockResolvedValue({ taskId: 't1', conversationId: 'c1', status: 'queued', kind: 'failed-run-fix' })
    render(wrap(<RunProblemPanel problem={problem} onClose={() => {}} onSendToCoder={() => ({ goal: failedRunGoal(goalInfo), origin: { page: 'noderunner', label: 'Workflows' } })} />))
    fireEvent.click(screen.getByRole('button', { name: /Fix with coder/ }))
    await waitFor(() => expect(aitaskApi.submit).toHaveBeenCalledTimes(1))
    const [kind, goal, context] = aitaskApi.submit.mock.calls[0]
    expect(kind).toBe('failed-run-fix')
    expect(goal).toContain('ex-9')
    expect(context).toEqual(['selection-text=run died\nHTTP: boom 500'])
    await waitFor(() => expect(openDraft.mock.calls[0][0].task.kind).toBe('failed-run-fix'))
  })

  it('coder off: says why, submits nothing, and the failure stays on screen', async () => {
    aitaskApi.status.mockResolvedValue({ enabled: false, reason: 'Coder mode is off.' })
    render(wrap(<RunProblemPanel problem={problem} onClose={() => {}} onSendToCoder={() => ({ goal: 'g', origin: {} })} />))
    fireEvent.click(screen.getByRole('button', { name: /Fix with coder/ }))
    expect(await screen.findByText(/Coder mode is off\./)).toBeInTheDocument()
    expect(aitaskApi.submit).not.toHaveBeenCalled()
    expect(screen.getByText('run died')).toBeInTheDocument()
  })

  it('without onSendToCoder there is no coder button', () => {
    render(wrap(<RunProblemPanel problem={problem} onClose={() => {}} />))
    expect(screen.queryByRole('button', { name: /Fix with coder/ })).toBeNull()
  })
})
