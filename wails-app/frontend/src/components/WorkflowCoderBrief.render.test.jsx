// @vitest-environment jsdom
import React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'

const { aitaskApi } = vi.hoisted(() => ({
  aitaskApi: { status: vi.fn(), submit: vi.fn(), startFill: vi.fn(), get: vi.fn(), cancel: vi.fn() },
}))
vi.mock('./SendToCoderApi.js', async (orig) => ({ ...(await orig()), aitaskApi }))

import WorkflowCoderBrief from './WorkflowCoderBrief.jsx'
import { CoderBubbleContext } from './bubbles/CoderBubbleContext.jsx'

const openDraft = vi.fn()
const navigate = vi.fn()
const renderBrief = () => render(
  <CoderBubbleContext.Provider value={{ openDraft, navigate }}>
    <WorkflowCoderBrief origin={{ page: 'noderunner', label: 'Workflows' }} />
  </CoderBubbleContext.Provider>,
)

beforeEach(() => { vi.clearAllMocks() })
afterEach(cleanup)

describe('WorkflowCoderBrief', () => {
  it('has nothing to send until the brief is written', () => {
    renderBrief()
    expect(screen.getByRole('button', { name: 'Build with coder' })).toBeDisabled()
    fireEvent.change(screen.getByLabelText(/describe what it should do/i), { target: { value: '   ' } })
    expect(screen.getByRole('button', { name: 'Build with coder' })).toBeDisabled()
  })

  it('submits the brief as a workflow-generate task with no context', async () => {
    aitaskApi.status.mockResolvedValue({ enabled: true })
    aitaskApi.submit.mockResolvedValue({ taskId: 't1', conversationId: 'c1', status: 'queued', kind: 'workflow-generate' })
    renderBrief()
    fireEvent.change(screen.getByLabelText(/describe what it should do/i), { target: { value: ' fetch the news every morning ' } })
    fireEvent.click(screen.getByRole('button', { name: /Build with coder/ }))
    await waitFor(() => expect(aitaskApi.submit).toHaveBeenCalledTimes(1))
    expect(aitaskApi.submit).toHaveBeenCalledWith('workflow-generate', 'fetch the news every morning', [], '')
    await waitFor(() => expect(openDraft).toHaveBeenCalledTimes(1))
    expect(openDraft.mock.calls[0][0].task.kind).toBe('workflow-generate')
  })

  it('coder off: explains, offers Settings and submits nothing', async () => {
    aitaskApi.status.mockResolvedValue({ enabled: false, reason: 'Coder mode is off.' })
    renderBrief()
    fireEvent.change(screen.getByLabelText(/describe what it should do/i), { target: { value: 'x' } })
    fireEvent.click(screen.getByRole('button', { name: /Build with coder/ }))
    expect(await screen.findByText(/Coder mode is off\./)).toBeInTheDocument()
    expect(aitaskApi.submit).not.toHaveBeenCalled()
  })
})
