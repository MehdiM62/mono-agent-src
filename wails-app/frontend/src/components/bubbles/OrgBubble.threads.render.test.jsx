// @vitest-environment jsdom
// An expanded org bubble (coder-bubble v2, Point 4): every role on the stage
// from the start, a thread per role (click a role), the Team timeline, and
// one AskCard for the workflow-HIL and send-approval asks, answered in the
// chat through the HIL queue.
import React, { useEffect } from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup, within, act, configure } from '@testing-library/react'

// The first render in a file costs about 3 s while modules warm up (0.5-1 s after), so the 1 s waitFor
// and 5 s test defaults fail on a loaded machine without anything being wrong.
configure({ asyncUtilTimeout: 8000 })
vi.setConfig({ testTimeout: 20000 })
import '../../i18n.js'
import history from './__fixtures__/acme-history.json'
import BUS from './__fixtures__/acme-bus.json'
import sendEmail from './__fixtures__/hil-send-email.json'
import sendDm from './__fixtures__/hil-send-dm.json'
import workflow from './__fixtures__/hil-workflow.json'

const { api, listeners, confirmMock } = vi.hoisted(() => ({
  listeners: { org: new Set(), run: new Set() },
  confirmMock: vi.fn(),
  api: {
    getOrgChatHistory: vi.fn(), getOrgLogs: vi.fn(), getOrgDesign: vi.fn(), getOrgStatus: vi.fn(),
    sendOrgChat: vi.fn(), answerOrgChat: vi.fn(), resolveOrgChat: vi.fn(), controlOrg: vi.fn(),
    streamOrgEvents: vi.fn(() => Promise.resolve({ ok: true })),
    stopOrgEvents: vi.fn(() => Promise.resolve({ ok: true })),
    getChatTurns: vi.fn(), coderStatus: vi.fn(),
    hilList: vi.fn(), hilResolve: vi.fn(), hilSendResolve: vi.fn(), hilSendAttachmentData: vi.fn(), openPathWithOS: vi.fn(), openURL: vi.fn(),
  },
}))
vi.mock('../../services/api.js', () => ({
  api,
  notify: vi.fn(),
  onChatEvent: () => () => {},
  onOrgEvent: cb => { listeners.org.add(cb); return () => listeners.org.delete(cb) },
  onOrgRunStatus: cb => { listeners.run.add(cb); return () => listeners.run.delete(cb) },
  newOrgEventsStreamId: (() => { let n = 0; return () => `s${++n}` })(),
}))
vi.mock('../ConfirmDialog.jsx', () => ({ confirm: confirmMock }))

import CoderBubbles from './CoderBubbles.jsx'
import { useCoderBubbles } from './useCoderBubbles.js'

function Harness({ org = 'acme' }) {
  const store = useCoderBubbles()
  useEffect(() => { store.openOrg(org, { expand: true }) }, []) // eslint-disable-line react-hooks/exhaustive-deps
  return <CoderBubbles store={store} onNavigate={() => {}} />
}

const node = id => document.querySelector(`[data-testid="stage-node"][data-agent="${id}"]`)
const tab = id => document.querySelector(`[data-thread-tab="${id}"]`)
const thread = () => screen.getByTestId('org-chat-thread')

beforeEach(() => {
  vi.clearAllMocks()
  listeners.org.clear()
  listeners.run.clear()
  localStorage.clear()
  Element.prototype.scrollTo = function scrollTo() {}
  api.getOrgChatHistory.mockResolvedValue(history)
  api.getOrgLogs.mockResolvedValue({ v: 1, items: BUS })
  api.getOrgDesign.mockResolvedValue({ org: { roles: history.roles } })
  api.coderStatus.mockResolvedValue({ enabled: false })
  api.hilList.mockResolvedValue([])
  api.hilResolve.mockResolvedValue({ ok: true, state: 'approved' })
  api.hilSendAttachmentData.mockResolvedValue('data:image/png;base64,AAAA')
  api.hilSendResolve.mockResolvedValue({ ok: true })
  confirmMock.mockResolvedValue(true)
})
afterEach(cleanup)

describe('the org stage', () => {
  it('shows every role from the start, before anyone has spoken', async () => {
    api.getOrgLogs.mockResolvedValue({ v: 1, items: [] })
    render(<Harness />)
    await screen.findByTestId('org-bubble-overlay')
    await waitFor(() => expect(node('qa')).toBeTruthy())
    expect(node('dev')).toBeTruthy()
    expect(node('lead')).toHaveTextContent('Chief')
  })
})

describe('per-role threads', () => {
  it('starts on the boss thread', async () => {
    render(<Harness />)
    await waitFor(() => expect(thread()).toHaveTextContent('Please ship the release notes today.'))
    expect(tab('boss')).toHaveAttribute('aria-selected', 'true')
    expect(thread()).toHaveAttribute('aria-label', 'Boss')
    expect(thread()).toHaveAttribute('aria-live', 'polite')
  })

  it('filters the thread to a role when it is clicked on the stage: its messages, tool activity and asks', async () => {
    render(<Harness />)
    await waitFor(() => expect(node('dev')).toBeTruthy())
    await waitFor(() => expect(thread()).toHaveTextContent('Please ship the release notes today.'))
    fireEvent.click(node('dev'))
    await waitFor(() => expect(tab('role')).toHaveTextContent('Developer'))
    expect(tab('role')).toHaveAttribute('aria-selected', 'true')
    // Dev's approval, its tool call and the team messages it sent and got.
    expect(screen.getByTestId('org-chat-approval')).toBeInTheDocument()
    expect(screen.getAllByTestId('org-chat-tool').length).toBeGreaterThan(0)
    expect(screen.getAllByTestId('org-chat-tool')[0]).toHaveTextContent('Edit')
    expect(screen.getAllByTestId('org-chat-team')).toHaveLength(2)
    // Not the person's message, the boss's lines, or other roles' questions.
    expect(thread()).not.toHaveTextContent('Please ship the release notes today.')
    expect(screen.queryByTestId('org-chat-question')).toBeNull()
    expect(screen.queryByTestId('org-chat-gate')).toBeNull()
    // Clicking it again, or the Boss tab, brings the whole thread back.
    fireEvent.click(tab('boss'))
    await waitFor(() => expect(thread()).toHaveTextContent('Please ship the release notes today.'))
    expect(tab('role')).toBeNull()
  })

  it('tells you what waits in the views you are not looking at', async () => {
    render(<Harness />)
    await waitFor(() => expect(node('dev')).toBeTruthy())
    await waitFor(() => expect(screen.getAllByTestId('org-chat-question')).toHaveLength(3))
    fireEvent.click(node('dev'))
    const hint = await screen.findByTestId('org-chat-waiting-elsewhere')
    expect(hint).toHaveTextContent('waiting for you in another view')
    fireEvent.click(hint)
    await waitFor(() => expect(screen.getByTestId('org-chat-gate')).toBeInTheDocument())
    expect(tab('boss')).toHaveAttribute('aria-selected', 'true')
  })

  it('shows the Team view as the role-to-role conversation, every row open', async () => {
    render(<Harness />)
    await waitFor(() => expect(thread()).toHaveTextContent('Please ship the release notes today.'))
    fireEvent.click(tab('team'))
    await waitFor(() => expect(screen.getAllByTestId('org-chat-team')).toHaveLength(3))
    expect(thread()).toHaveAttribute('aria-label', 'Team')
    expect(thread()).toHaveTextContent('Draft ready in NOTES.md')
    expect(thread()).not.toHaveTextContent('Please ship the release notes today.')
    expect(screen.queryByTestId('org-chat-question')).toBeNull()
  })

  it('moves between the tabs with the arrow keys', async () => {
    render(<Harness />)
    await waitFor(() => expect(tab('boss')).toBeTruthy())
    fireEvent.keyDown(tab('boss'), { key: 'ArrowRight' })
    await waitFor(() => expect(tab('team')).toHaveAttribute('aria-selected', 'true'))
    fireEvent.keyDown(tab('team'), { key: 'ArrowLeft' })
    await waitFor(() => expect(tab('boss')).toHaveAttribute('aria-selected', 'true'))
    expect(screen.getByRole('tablist')).toBeInTheDocument()
  })

  it('says so when a role has done nothing yet', async () => {
    api.getOrgChatHistory.mockResolvedValue({ ...history, items: history.items.filter(i => i.role !== 'qa') })
    api.getOrgLogs.mockResolvedValue({ v: 1, items: [] })
    render(<Harness />)
    await waitFor(() => expect(node('qa')).toBeTruthy())
    fireEvent.click(node('qa'))
    expect(await screen.findByText('QA has not said or done anything yet.')).toBeInTheDocument()
  })
})

describe('asks from the HIL queue in an org bubble', () => {
  const ask = [sendEmail, sendDm, { ...workflow }]

  it('shows the send approval of a role and a tagged workflow item as cards in the boss thread', async () => {
    api.hilList.mockResolvedValue(ask)
    render(<Harness />)
    const sends = await screen.findAllByTestId('org-chat-send')
    // The DM was prepared by qa, who is also a role of acme: it shows too.
    expect(sends).toHaveLength(2)
    expect(sends[0]).toHaveTextContent('Sam Rivera')
    expect(screen.getByTestId('org-chat-hil')).toHaveTextContent('Weekly outreach')
  })

  it('approves a send with the edited text through the HIL queue, once', async () => {
    api.hilList.mockResolvedValue([sendEmail])
    render(<Harness />)
    const card = await screen.findByTestId('org-chat-send')
    fireEvent.click(within(card).getByTestId('ask-edit'))
    fireEvent.change(within(card).getByTestId('ask-text-edit'), { target: { value: 'Short note.' } })
    const approve = within(card).getByTestId('ask-approve')
    fireEvent.click(approve)
    fireEvent.click(approve)
    expect(api.hilSendResolve).toHaveBeenCalledTimes(1)
    expect(api.hilSendResolve).toHaveBeenCalledWith('hil-send-1', true, { editText: 'Short note.' })
    await waitFor(() => expect(screen.getByTestId('org-chat-send')).toHaveAttribute('data-pending', 'false'))
    expect(screen.getByTestId('org-chat-send')).toHaveTextContent('approved')
  })

  it('rejects a send, and shows a refusal without losing the card', async () => {
    api.hilList.mockResolvedValue([sendEmail])
    api.hilSendResolve.mockResolvedValueOnce({ error: 'account signed out' })
    render(<Harness />)
    const card = await screen.findByTestId('org-chat-send')
    fireEvent.click(within(card).getByTestId('ask-reject'))
    await waitFor(() => expect(within(screen.getByTestId('org-chat-send')).getByRole('alert')).toHaveTextContent('account signed out'))
    expect(api.hilSendResolve).toHaveBeenLastCalledWith('hil-send-1', false, { editText: '' })
    expect(screen.getByTestId('org-chat-send')).toHaveAttribute('data-pending', 'true')
  })

  it('files a send under the role that prepared it', async () => {
    api.hilList.mockResolvedValue([sendEmail, sendDm])
    render(<Harness />)
    await screen.findAllByTestId('org-chat-send')
    await waitFor(() => expect(node('dev')).toBeTruthy())
    fireEvent.click(node('dev'))
    await waitFor(() => expect(tab('role')).toHaveTextContent('Developer'))
    expect(screen.getAllByTestId('org-chat-send')).toHaveLength(1)
    expect(screen.getByTestId('org-chat-send')).toHaveTextContent('Sam Rivera')
    fireEvent.click(node('qa'))
    await waitFor(() => expect(tab('role')).toHaveTextContent('QA'))
    expect(screen.getAllByTestId('org-chat-send')).toHaveLength(1)
    expect(screen.getByTestId('org-chat-send')).toHaveTextContent('Jo Park')
  })

  it('approves a workflow item with its edited fields', async () => {
    api.hilList.mockResolvedValue([workflow])
    render(<Harness />)
    const card = await screen.findByTestId('org-chat-hil')
    fireEvent.change(within(card).getByRole('textbox'), { target: { value: 'Edited' } })
    fireEvent.click(within(card).getByTestId('ask-approve'))
    await waitFor(() => expect(api.hilResolve).toHaveBeenCalledWith('hil-wf-1', { approve: true, editText: undefined, data: { caption: 'Edited' } }))
  })

  it('lets a pending send be answered even when the org is not running', async () => {
    api.getOrgChatHistory.mockResolvedValue({ ...history, status: 'stopped' })
    api.hilList.mockResolvedValue([sendEmail])
    render(<Harness />)
    const card = await screen.findByTestId('org-chat-send')
    await waitFor(() => expect(within(card).getByTestId('ask-approve')).not.toBeDisabled())
    // ...while the org's own gate stays locked.
    expect(within(screen.getByTestId('org-chat-gate')).getByTestId('org-chat-approve')).toBeDisabled()
  })

  it('keeps the thread a live region', async () => {
    api.hilList.mockResolvedValue([sendEmail])
    render(<Harness />)
    await screen.findByTestId('org-chat-send')
    expect(thread()).toHaveAttribute('role', 'log')
    await act(async () => {})
  })
})
