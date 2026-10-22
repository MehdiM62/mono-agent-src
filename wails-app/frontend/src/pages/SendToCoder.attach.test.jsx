// @vitest-environment jsdom
import React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'
import '../i18n.js'

// Every place that offers "Send to coder" renders the button and, on click,
// hands the coder the right goal and context. Only the AI-task bindings and
// each page's own data calls are faked.
const { aitaskApi, App, api } = vi.hoisted(() => ({
  aitaskApi: { status: vi.fn(), submit: vi.fn(), get: vi.fn(), cancel: vi.fn() },
  App: {
    GetProfileInfoFields: vi.fn(), GetProfileInfo: vi.fn(), SetProfileInfo: vi.fn(), ClearProfileInfo: vi.fn(),
    RevealProfileInfo: vi.fn(), SetProfileInfoPhoto: vi.fn(), GetProfileInfoPhoto: vi.fn(), AIFillProfileInfo: vi.fn(),
    OpenAnyFilePicker: vi.fn(),
    OpenJSONFilePicker: vi.fn(), ReadJSONFile: vi.fn(), EvaluateApplication: vi.fn(), HasGeneratedDocuments: vi.fn(),
    ApplyToApplication: vi.fn(), SendApplication: vi.fn(), SetApplicationStatus: vi.fn(),
    GetApplications: vi.fn(), GetApplication: vi.fn(), GetApplicationAssets: vi.fn(), AddApplication: vi.fn(),
    OpenURL: vi.fn(), OpenPathWithOS: vi.fn(), GetProfileDocumentData: vi.fn(), GetProfileDocumentText: vi.fn(),
  },
  api: { analyzeRecording: vi.fn(), verifyDraft: vi.fn(), saveDraft: vi.fn() },
}))
vi.mock('../wailsjs/go/main/App', () => App)
vi.mock('../services/api.js', () => ({ api, notify: vi.fn() }))
vi.mock('../components/SendToCoderApi.js', async (orig) => ({ ...(await orig()), aitaskApi }))
vi.mock('../components/ConfirmDialog.jsx', () => ({ confirm: vi.fn(() => Promise.resolve(true)) }))

import Info from './Info.jsx'
import Logs from './Logs.jsx'
import RecordingReview from './connections/RecordingReview.jsx'
import ApplicationsProcessPendingFlow from './ApplicationsProcessPendingFlow.jsx'
import Applications from './Applications.jsx'
import RunProblemPanel from '../components/RunProblemPanel.jsx'
import { CoderBubbleContext } from '../components/bubbles/CoderBubbleContext.jsx'
import { mockApplicationDetail, mockApplicationAssets } from '../components/applications/fixtures.js'

const openDraft = vi.fn()
const wrap = (ui) => <CoderBubbleContext.Provider value={{ openDraft, navigate: vi.fn() }}>{ui}</CoderBubbleContext.Provider>

beforeEach(() => {
  vi.clearAllMocks()
  Element.prototype.scrollIntoView = function scrollIntoView() {} // jsdom has none; Logs follows new lines
  aitaskApi.status.mockResolvedValue({ enabled: true })
  aitaskApi.submit.mockResolvedValue({ taskId: 't1', conversationId: 'c1', status: 'queued', goal: 'g' })
})
afterEach(cleanup)

async function clickAndGetSubmit(name) {
  fireEvent.click(await screen.findByRole('button', { name }))
  await waitFor(() => expect(aitaskApi.submit).toHaveBeenCalledTimes(1))
  await waitFor(() => expect(openDraft).toHaveBeenCalledTimes(1))
  return aitaskApi.submit.mock.calls[0] // [kind, goal, context, applicationId]
}

describe('Send to coder attach points', () => {
  it('Logs: an error row offers "Ask coder about this" with that row as selection text; other rows do not', async () => {
    const logs = [
      { time: '10:00', level: 'INFO', source: 'sync', message: 'all good' },
      { time: '10:01', level: 'ERROR', source: 'sync', message: 'token expired' },
    ]
    render(wrap(<Logs logs={logs} onClear={() => {}} onRefresh={() => {}} />))
    expect(screen.getAllByRole('button', { name: /Ask coder about this/ })).toHaveLength(1)
    const [kind, , context] = await clickAndGetSubmit(/Ask coder about this/)
    expect(kind).toBe('generic')
    expect(context).toEqual(['selection-text=[sync] token expired'])
  })

  it('RunProblemPanel: "Fix with coder" appears only with onSendToCoder and sends the failed nodes and errors', async () => {
    const problem = { status: 'FAILED', error: 'run died', failed: [{ id: 'n1', name: 'HTTP', error: 'boom 500' }] }
    const { unmount } = render(wrap(<RunProblemPanel problem={problem} onClose={() => {}} />))
    expect(screen.queryByRole('button', { name: /Fix with coder/ })).toBeNull()
    unmount()
    const onSendToCoder = vi.fn(() => ({ goal: 'fix the run', origin: { page: 'noderunner', label: 'Workflows' } }))
    render(wrap(<RunProblemPanel problem={problem} onClose={() => {}} onSendToCoder={onSendToCoder} />))
    const [, goal, context] = await clickAndGetSubmit(/Fix with coder/)
    expect(goal).toBe('fix the run')
    expect(context).toEqual(['selection-text=run died\nHTTP: boom 500'])
    expect(openDraft.mock.calls[0][0].task.origin).toEqual({ page: 'noderunner', label: 'Workflows' })
  })

  it('Info: the AI-fill row has "Fill with coder"', async () => {
    App.GetProfileInfoFields.mockResolvedValue({ groups: [], fields: [] })
    App.GetProfileInfo.mockResolvedValue({ entries: [] })
    render(wrap(<Info />))
    const [kind, goal, context] = await clickAndGetSubmit(/Fill with coder/)
    expect(kind).toBe('generic')
    expect(goal).toMatch(/profile information/)
    expect(context).toEqual([])
  })

  it('RecordingReview: "Discuss with coder" names the recording, never its content', async () => {
    api.analyzeRecording.mockResolvedValue({ draft: { actionDef: { steps: [] } }, draftDir: '/d' })
    render(wrap(<RecordingReview recording={{ id: 'rec-1', title: 'Login flow' }} automationId="auto-1" onBack={() => {}} />))
    const [, , context] = await clickAndGetSubmit(/Discuss with coder/)
    expect(context).toEqual(['selection-text=Recording rec-1: Login flow; automation auto-1'])
  })

  it('Process pending: after the evaluation, "Discuss in coder" carries the application and the verdict', async () => {
    App.EvaluateApplication.mockResolvedValue({ verdict: 'GOOD', overall_score: 8, rationale: 'solid fit' })
    App.HasGeneratedDocuments.mockResolvedValue(false)
    render(wrap(<ApplicationsProcessPendingFlow pendingApplications={[{ id: 'app-1', title: 'Backend Engineer', company: 'Acme' }]} onClose={() => {}} />))
    fireEvent.click(screen.getByRole('button', { name: 'Start' }))
    const [, , context, applicationId] = await clickAndGetSubmit(/Discuss in coder/)
    expect(applicationId).toBe('app-1')
    expect(context[0]).toContain('Backend Engineer at Acme')
    expect(context[0]).toContain('solid fit')
    // The Apply-with-coder button belongs to a later task.
    expect(screen.queryByRole('button', { name: /apply with coder/i })).toBeNull()
  })

  it('Applications: the open detail has "Discuss in coder" for that application', async () => {
    App.GetApplications.mockResolvedValue([{ id: 'app-1', title: 'Lead Architect', company: 'OmniCorp', kind: 'job', status: 'pending', tags: [], updated_at: 'x' }])
    App.GetApplication.mockResolvedValue(mockApplicationDetail)
    App.GetApplicationAssets.mockResolvedValue(mockApplicationAssets)
    App.GetProfileDocumentData.mockResolvedValue('')
    App.GetProfileDocumentText.mockResolvedValue('')
    render(wrap(<Applications />))
    expect(screen.queryByRole('button', { name: /Discuss in coder/ })).toBeNull()
    fireEvent.click(await screen.findByText('Lead Architect'))
    const [, , , applicationId] = await clickAndGetSubmit(/Discuss in coder/)
    expect(applicationId).toBe('app-1')
  })
})
