// @vitest-environment jsdom
import React from 'react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, cleanup } from '@testing-library/react'
import '../i18n.js'

// While failed-run-fix and recording-analyze run unconfined, their buttons are
// not offered (lib/confinedKinds.js). This file runs with the real flag.
const { App, api } = vi.hoisted(() => ({
  App: {},
  api: { analyzeRecording: vi.fn(), verifyDraft: vi.fn(), saveDraft: vi.fn() },
}))
vi.mock('../wailsjs/go/main/App', () => App)
vi.mock('../services/api.js', () => ({ api, notify: vi.fn() }))

import RunProblemPanel from './RunProblemPanel.jsx'
import RecordingReview from '../pages/connections/RecordingReview.jsx'
import { UNCONFINED_KIND_BUTTONS } from '../lib/confinedKinds.js'

afterEach(() => cleanup())

describe('buttons of kinds that run unconfined', () => {
  it('the flag is off', () => {
    expect(UNCONFINED_KIND_BUTTONS).toBe(false)
  })

  it('RunProblemPanel offers no "Fix with coder" even when a coder handler is given, and never calls it', () => {
    const onSendToCoder = vi.fn(() => ({ goal: 'fix', origin: { page: 'noderunner', label: 'Workflows' } }))
    const problem = { status: 'FAILED', error: 'run died', failed: [{ id: 'n1', name: 'HTTP', error: 'boom' }] }
    render(<RunProblemPanel problem={problem} onClose={() => {}} onSendToCoder={onSendToCoder} workflowId="wf" executionId="ex" />)
    expect(screen.getByTestId('run-problem')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Fix with coder/ })).toBeNull()
    expect(onSendToCoder).not.toHaveBeenCalled()
  })

  it('RecordingReview offers no "Discuss with coder"', () => {
    api.analyzeRecording.mockResolvedValue({ draft: { actionDef: { steps: [] } }, draftDir: '/d' })
    render(<RecordingReview recording={{ id: 'rec-1', title: 'Login flow' }} automationId="auto-1" onBack={() => {}} />)
    expect(screen.queryByRole('button', { name: /Discuss with coder/ })).toBeNull()
  })
})
