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
  aitaskApi: { status: vi.fn(), submit: vi.fn(), startFill: vi.fn(), get: vi.fn(), cancel: vi.fn() },
  App: {
    GetProfileInfoFields: vi.fn(), GetProfileInfo: vi.fn(), SetProfileInfo: vi.fn(), ClearProfileInfo: vi.fn(),
    RevealProfileInfo: vi.fn(), SetProfileInfoPhoto: vi.fn(), GetProfileInfoPhoto: vi.fn(), AIFillProfileInfo: vi.fn(),
    OpenAnyFilePicker: vi.fn(),
    OpenJSONFilePicker: vi.fn(), ReadJSONFile: vi.fn(), EvaluateApplication: vi.fn(), HasGeneratedDocuments: vi.fn(),
    ApplyToApplication: vi.fn(), SendApplication: vi.fn(), SetApplicationStatus: vi.fn(),
    GetApplications: vi.fn(), GetApplication: vi.fn(), GetApplicationAssets: vi.fn(), AddApplication: vi.fn(),
    StartApplicationQueue: vi.fn(), GetApplicationTab: vi.fn(), ConfirmApplicationSubmitted: vi.fn(),
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
    const [kind, goal, context] = await clickAndGetSubmit(/Fix with coder/)
    expect(kind).toBe('failed-run-fix')
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
    const header = await screen.findByRole('banner', { name: 'Application header' })
    expect(header).toContainElement(screen.getByRole('button', { name: /Discuss in coder/ }))
    const [, , , applicationId] = await clickAndGetSubmit(/Discuss in coder/)
    expect(applicationId).toBe(mockApplicationDetail.id)
  })

  it('Applications: the status filter lists awaiting submit and needs you', async () => {
    App.GetApplications.mockResolvedValue([
      { id: 'a1', title: 'Parked One', company: 'X', kind: 'job', status: 'awaiting_submit', tags: [], updated_at: 'x' },
      { id: 'a2', title: 'Stuck Two', company: 'Y', kind: 'job', status: 'needs_you', tags: [], updated_at: 'x' },
    ])
    render(wrap(<Applications />))
    fireEvent.click(await screen.findByRole('button', { name: 'awaiting submit' }))
    expect(await screen.findByText('Parked One')).toBeInTheDocument()
    expect(screen.queryByText('Stuck Two')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'needs you' }))
    expect(await screen.findByText('Stuck Two')).toBeInTheDocument()
  })
})

describe('Apply with coder', () => {
  const fill = { taskId: 't9', conversationId: 'c9', status: 'queued', goal: 'Fill in the form', applicationId: 'app-1' }
  const flow = () => render(wrap(<ApplicationsProcessPendingFlow pendingApplications={[{ id: 'app-1', title: 'Backend Engineer', company: 'Acme' }]} onClose={() => {}} />))
  async function evaluated() {
    App.EvaluateApplication.mockResolvedValue({ verdict: 'GOOD', overall_score: 8, rationale: 'solid fit' })
    App.HasGeneratedDocuments.mockResolvedValue(false)
    flow()
    fireEvent.click(screen.getByRole('button', { name: 'Start' }))
    return screen.findByRole('button', { name: 'Apply with coder' })
  }

  it('Process pending: starts the fill, opens its chat, and the header will say filled, waiting for you to submit', async () => {
    aitaskApi.startFill.mockResolvedValue(fill)
    fireEvent.click(await evaluated())
    await waitFor(() => expect(openDraft).toHaveBeenCalledTimes(1))
    expect(aitaskApi.startFill).toHaveBeenCalledWith('app-1')
    expect(aitaskApi.submit).not.toHaveBeenCalled()
    const seed = openDraft.mock.calls[0][0]
    expect(seed.conversationId).toBe('c9')
    expect(seed.task).toMatchObject({ taskId: 't9', doneLabel: 'filled, waiting for you to submit' })
    expect(JSON.stringify(seed)).not.toMatch(/submitted/i)
  })

  it('coder disabled: says why, starts nothing, and the regular Apply is still there', async () => {
    aitaskApi.status.mockResolvedValue({ enabled: false, reason: 'Coder mode is off.' })
    fireEvent.click(await evaluated())
    expect(await screen.findByText(/Coder mode is off\./)).toBeInTheDocument()
    expect(aitaskApi.startFill).not.toHaveBeenCalled()
    expect(openDraft).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Apply' })).toBeEnabled()
  })

  it('a fallback from the fill start opens nothing', async () => {
    aitaskApi.startFill.mockResolvedValue({ fallback: 'oneshot', reason: 'turned off just now' })
    fireEvent.click(await evaluated())
    expect(await screen.findByText(/turned off just now/)).toBeInTheDocument()
    expect(openDraft).not.toHaveBeenCalled()
  })

  it('detail header: Discuss in coder is in the header, Apply with coder only while the application is being worked on', async () => {
    App.GetApplication.mockResolvedValue({ ...mockApplicationDetail, id: 'app-1', status: 'pending' })
    App.GetApplicationAssets.mockResolvedValue(mockApplicationAssets)
    App.GetApplications.mockResolvedValue([{ id: 'app-1', title: 'Lead Architect', company: 'OmniCorp', kind: 'job', status: 'pending', tags: [], updated_at: 'x' }])
    aitaskApi.startFill.mockResolvedValue(fill)
    render(wrap(<Applications />))
    fireEvent.click(await screen.findByText('Lead Architect'))
    const header = await screen.findByRole('banner', { name: 'Application header' })
    expect(header).toContainElement(screen.getByRole('button', { name: /Discuss in coder/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Apply with coder' }))
    await waitFor(() => expect(aitaskApi.startFill).toHaveBeenCalledWith('app-1'))
  })

  it('detail header: no Apply with coder once the application is applied', async () => {
    App.GetApplication.mockResolvedValue({ ...mockApplicationDetail, id: 'app-1', status: 'applied' })
    App.GetApplicationAssets.mockResolvedValue(mockApplicationAssets)
    App.GetApplications.mockResolvedValue([{ id: 'app-1', title: 'Lead Architect', company: 'OmniCorp', kind: 'job', status: 'pending', tags: [], updated_at: 'x' }])
    render(wrap(<Applications />))
    fireEvent.click(await screen.findByText('Lead Architect'))
    await screen.findByRole('banner', { name: 'Application header' })
    expect(screen.queryByRole('button', { name: 'Apply with coder' })).toBeNull()
  })
})

describe('Applications list: apply selected with coder', () => {
  const apps = [
    { id: 'p1', title: 'Pending One', company: 'X', kind: 'job', status: 'pending', tags: [], updated_at: 'x' },
    { id: 'p2', title: 'Pending Two', company: 'Y', kind: 'job', status: 'pending', tags: [], updated_at: 'x' },
    { id: 'w1', title: 'Parked One', company: 'Z', kind: 'job', status: 'awaiting_submit', tags: [], updated_at: 'x' },
  ]

  it('ticks pending applications, starts the queue for exactly those, and parked rows say they wait for the person', async () => {
    App.GetApplications.mockResolvedValue(apps)
    App.StartApplicationQueue.mockResolvedValue(undefined)
    render(wrap(<Applications />))
    const apply = await screen.findByRole('button', { name: /Apply selected with coder/ })
    expect(apply).toBeDisabled()
    fireEvent.click(screen.getByLabelText('Select Pending One to apply with coder'))
    fireEvent.click(screen.getByLabelText('Select Pending Two to apply with coder'))
    fireEvent.click(screen.getByLabelText('Select Pending One to apply with coder'))
    fireEvent.click(screen.getByRole('button', { name: 'Apply selected with coder (1)' }))
    await waitFor(() => expect(App.StartApplicationQueue).toHaveBeenCalledWith(['p2'], 0))
    expect(aitaskApi.status).toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'awaiting submit' }))
    expect(await screen.findByText('waiting for your submit')).toBeInTheDocument()
    expect(screen.queryByLabelText(/Select Parked One/)).toBeNull()
  })

  it('the detail of a parked application shows the waiting state; Confirm reloads the application', async () => {
    App.GetApplications.mockResolvedValue([{ id: 'w1', title: 'Parked One', company: 'Z', kind: 'job', status: 'awaiting_submit', tags: [], updated_at: 'x' }])
    App.GetApplication.mockResolvedValue({ ...mockApplicationDetail, id: 'w1', status: 'awaiting_submit' })
    App.GetApplicationAssets.mockResolvedValue(mockApplicationAssets)
    App.GetApplicationTab.mockResolvedValue({ applicationId: 'w1', tab: null, submitObservedAt: '2026-10-22T10:00:00Z' })
    App.ConfirmApplicationSubmitted.mockResolvedValue(undefined)
    render(wrap(<Applications />))
    fireEvent.click(await screen.findByRole('button', { name: 'awaiting submit' }))
    fireEvent.click(await screen.findByText('Parked One'))
    expect(await screen.findByText('Waiting for your submit')).toBeInTheDocument()
    const before = App.GetApplication.mock.calls.length
    fireEvent.click(await screen.findByRole('button', { name: 'Confirm' }))
    await waitFor(() => expect(App.GetApplication.mock.calls.length).toBeGreaterThan(before))
    expect(App.ConfirmApplicationSubmitted).toHaveBeenCalledWith('w1')
  })
})
