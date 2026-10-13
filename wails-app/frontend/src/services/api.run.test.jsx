// @vitest-environment jsdom
// Starting a run answers at once: a refused run resolves { ok: false, error }
// (no 60 s wait for an execution that never starts), and an inactive
// workflow's toast offers "Activate & run" — the activation is the user's
// click, never automatic.
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, cleanup, fireEvent, act } from '@testing-library/react'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (k, o) => (o?.op ? `${k}:${o.op}` : k) }),
}))
vi.mock('../wailsjs/go/main/App', () => ({
  RunWorkflow: vi.fn(),
  RunWorkflowWithInput: vi.fn(),
  GetExtensionStatus: vi.fn(),
}))
vi.mock('../wailsjs/runtime/runtime', () => ({ EventsOn: vi.fn(() => () => {}) }))

import * as GoApp from '../wailsjs/go/main/App'
import { api, reportRunFailure, onApiError } from './api.js'
import Toasts from '../components/Toasts.jsx'

afterEach(() => { cleanup(); vi.clearAllMocks() })

describe('run start', () => {
  it('resolves ok when the binding accepts the run', async () => {
    GoApp.RunWorkflowWithInput.mockResolvedValue(undefined)
    await expect(api.runWorkflowWithInput('wf1', '{"prompt":"x"}')).resolves.toEqual({ ok: true })
    expect(GoApp.RunWorkflowWithInput).toHaveBeenCalledWith('wf1', '{"prompt":"x"}')
  })

  it('resolves the refusal immediately instead of a swallowed string', async () => {
    GoApp.RunWorkflow.mockRejectedValue('workflow "Gemini" is inactive — activate it first')
    await expect(api.runWorkflow('wf1')).resolves.toEqual({ ok: false, error: 'workflow "Gemini" is inactive — activate it first' })
    GoApp.RunWorkflowWithInput.mockRejectedValue(new Error('workflow wf1 is already running'))
    await expect(api.runWorkflowWithInput('wf1')).resolves.toEqual({ ok: false, error: 'workflow wf1 is already running' })
  })
})

describe('reportRunFailure', () => {
  it('offers Activate & run only for an inactive workflow, and runs it on click', async () => {
    const seen = []
    const off = onApiError(d => seen.push(d))
    const activateAndRun = vi.fn()
    reportRunFailure('workflow wf1 is already running', { activateAndRun })
    expect(seen[0].action).toBeNull()

    render(<Toasts />)
    act(() => reportRunFailure('workflow "Gemini" is inactive — activate it first', { activateAndRun }))
    off()
    expect(screen.getByText(/is inactive/)).toBeInTheDocument()
    expect(activateAndRun).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Activate & run' }))
    expect(activateAndRun).toHaveBeenCalledTimes(1)
    expect(screen.queryByText(/is inactive/)).not.toBeInTheDocument()
  })
})

describe('getExtensionStatus', () => {
  it('parses the CLI JSON and stays quiet on failure', async () => {
    GoApp.GetExtensionStatus.mockResolvedValue('{"running":true,"clientPairing":"mismatch"}')
    await expect(api.getExtensionStatus()).resolves.toEqual({ running: true, clientPairing: 'mismatch' })
    GoApp.GetExtensionStatus.mockRejectedValue(new Error('boom'))
    await expect(api.getExtensionStatus()).resolves.toBeNull()
  })
})
