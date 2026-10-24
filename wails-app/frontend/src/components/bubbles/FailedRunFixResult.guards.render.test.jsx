// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'
import '../../i18n.js'

const { aitaskApplyApi, aitaskApi, confirm } = vi.hoisted(() => ({
  aitaskApplyApi: { preview: vi.fn(), apply: vi.fn(), saveDraft: vi.fn() },
  aitaskApi: { get: vi.fn(), cancel: vi.fn(), status: vi.fn(), submit: vi.fn() },
  confirm: vi.fn(),
}))
vi.mock('../SendToCoderApi.js', async (orig) => ({ ...(await orig()), aitaskApplyApi, aitaskApi }))
vi.mock('../settings/aitaskBudgetApi.js', () => ({ aitaskBudgetApi: { forTask: vi.fn(() => Promise.resolve(null)) } }))
vi.mock('../ConfirmDialog.jsx', () => ({ confirm }))

import { TaskHeaderCard } from './TaskHeaderCard.jsx'
import { TaskResultLine } from './TaskResultLine.jsx'
import { CoderBubbleContext } from './CoderBubbleContext.jsx'

const result = { diagnosis: 'd', workflowId: 'w1', executionId: 'e1', fix: { kind: 'workflow-edit', description: 'x', edits: [{ node: 'n1', config: {} }] } }
const preview = (over = {}) => ({ ok: true, workflowId: 'w1', workflowName: 'WF', edits: [{ node: 'n1', nodeName: 'N', path: 'url', before: 'a', after: 'b' }], warnings: [], ...over })
const mount = (extra = {}) => render(<CoderBubbleContext.Provider value={{ openDraft: vi.fn(), navigate: vi.fn() }}><TaskResultLine kind="failed-run-fix" taskId="t1" result={result} {...extra} /></CoderBubbleContext.Provider>)
const applyBtn = () => screen.getByRole('button', { name: /Apply fix|Applying|^Applied$/ })

beforeEach(() => { confirm.mockResolvedValue(true) })
afterEach(() => { cleanup(); vi.clearAllMocks() })

describe('nothing applies without a click', () => {
  it('opening the header on a complete task only previews', async () => {
    aitaskApplyApi.preview.mockResolvedValue(preview())
    aitaskApi.get.mockResolvedValue({ status: 'complete', kind: 'failed-run-fix', result })
    render(<TaskHeaderCard task={{ taskId: 't1', kind: 'failed-run-fix', goal: 'g', status: 'complete' }} />)
    await screen.findByTestId('fix-diff')
    expect(aitaskApplyApi.apply).not.toHaveBeenCalled()
    expect(aitaskApplyApi.saveDraft).not.toHaveBeenCalled()
    expect(confirm).not.toHaveBeenCalled()
  })

  it('rendering a generated workflow never saves it', async () => {
    aitaskApi.get.mockResolvedValue({ status: 'complete', kind: 'workflow-generate', result: { summary: 's', workflow: { name: 'n', nodes: [{}] }, validation: { valid: true } } })
    render(<TaskHeaderCard task={{ taskId: 't1', kind: 'workflow-generate', goal: 'g', status: 'complete' }} />)
    await screen.findByRole('button', { name: 'Save as draft' })
    expect(aitaskApplyApi.saveDraft).not.toHaveBeenCalled()
  })
})

describe('Apply is disabled', () => {
  it('while the preview loads, and a click calls nothing', async () => {
    aitaskApplyApi.preview.mockReturnValue(new Promise(() => {}))
    mount()
    expect(applyBtn()).toBeDisabled()
    fireEvent.click(applyBtn())
    expect(confirm).not.toHaveBeenCalled()
    expect(aitaskApplyApi.apply).not.toHaveBeenCalled()
  })

  it('when the preview has errors, and a click calls nothing', async () => {
    aitaskApplyApi.preview.mockResolvedValue(preview({ ok: false, error: 'node gone' }))
    mount()
    await screen.findByText('node gone')
    fireEvent.click(applyBtn())
    expect(confirm).not.toHaveBeenCalled()
    expect(aitaskApplyApi.apply).not.toHaveBeenCalled()
  })

  it('while an apply is in flight: a second click does not apply twice', async () => {
    aitaskApplyApi.preview.mockResolvedValue(preview())
    let finish
    aitaskApplyApi.apply.mockReturnValue(new Promise(r => { finish = r }))
    mount()
    await screen.findByTestId('fix-diff')
    fireEvent.click(applyBtn())
    await waitFor(() => expect(aitaskApplyApi.apply).toHaveBeenCalledTimes(1))
    expect(applyBtn()).toBeDisabled()
    fireEvent.click(applyBtn())
    finish({ ok: true, applied: 1, workflowId: 'w1' })
    await screen.findByText(/Applied 1 edits/)
    expect(aitaskApplyApi.apply).toHaveBeenCalledTimes(1)
    expect(confirm).toHaveBeenCalledTimes(1)
  })

  it('while the confirm dialog is open: a second click does not open another', async () => {
    aitaskApplyApi.preview.mockResolvedValue(preview())
    let answer
    confirm.mockReturnValue(new Promise(r => { answer = r }))
    mount()
    await screen.findByTestId('fix-diff')
    fireEvent.click(applyBtn())
    fireEvent.click(applyBtn())
    expect(confirm).toHaveBeenCalledTimes(1)
    answer(false)
    await waitFor(() => expect(applyBtn()).toBeEnabled())
    expect(aitaskApplyApi.apply).not.toHaveBeenCalled()
  })
})

describe('warning codes', () => {
  it('an unknown code shows the CLI text plainly', async () => {
    aitaskApplyApi.preview.mockResolvedValue(preview({ warnings: [{ code: 'something_new', text: 'The CLI says hello' }] }))
    mount()
    expect(await screen.findByText('The CLI says hello')).toBeInTheDocument()
  })
})

describe('typed refusals', () => {
  const clickApply = async () => { await screen.findByTestId('fix-diff'); fireEvent.click(applyBtn()) }

  it('stale_proposal: says to ask again and offers the Fix with coder flow', async () => {
    aitaskApplyApi.preview.mockResolvedValue(preview())
    aitaskApplyApi.apply.mockRejectedValue(new Error('stale_proposal: the workflow differs'))
    mount()
    await clickApply()
    expect(await screen.findByRole('alert')).toHaveTextContent('The workflow changed since this was proposed: ask again.')
    aitaskApi.status.mockResolvedValue({ enabled: true })
    aitaskApi.submit.mockResolvedValue({ taskId: 't2', conversationId: 'c2', status: 'queued', goal: 'g' })
    fireEvent.click(screen.getByRole('button', { name: /Ask again/ }))
    await waitFor(() => expect(aitaskApi.submit).toHaveBeenCalled())
    expect(aitaskApi.submit.mock.calls[0][0]).toBe('failed-run-fix')
    expect(aitaskApi.submit.mock.calls[0][2]).toEqual(['selection-text=workflow: w1\nexecution: e1'])
  })

  it('already_applied: shows Applied, disabled', async () => {
    aitaskApplyApi.preview.mockResolvedValue(preview())
    aitaskApplyApi.apply.mockRejectedValue(new Error('already_applied: done'))
    mount()
    await clickApply()
    await waitFor(() => expect(applyBtn()).toHaveTextContent('Applied'))
    expect(applyBtn()).toBeDisabled()
  })

  it('operator_only: plain wording', async () => {
    aitaskApplyApi.preview.mockResolvedValue(preview())
    aitaskApplyApi.apply.mockRejectedValue(new Error('operator_only: nope'))
    mount()
    await clickApply()
    expect(await screen.findByRole('alert')).toHaveTextContent('Only you can apply this, from the app window.')
  })

  it('an active workflow: a second confirm, and the allow flag only after it', async () => {
    aitaskApplyApi.preview.mockResolvedValue(preview({ warnings: [{ code: 'workflow_active', text: 'active' }] }))
    aitaskApplyApi.apply.mockResolvedValue({ ok: true, applied: 1, workflowId: 'w1' })
    mount()
    await clickApply()
    await screen.findByText(/Applied 1 edits/)
    expect(confirm).toHaveBeenCalledTimes(2)
    expect(confirm.mock.calls[1][0]).toMatch(/running or scheduled/)
    expect(aitaskApplyApi.apply).toHaveBeenCalledWith('t1', true)
  })

  it('an active workflow, second confirm declined: nothing is applied', async () => {
    aitaskApplyApi.preview.mockResolvedValue(preview({ warnings: [{ code: 'workflow_active', text: 'active' }] }))
    confirm.mockResolvedValueOnce(true).mockResolvedValueOnce(false)
    mount()
    await clickApply()
    await waitFor(() => expect(confirm).toHaveBeenCalledTimes(2))
    expect(aitaskApplyApi.apply).not.toHaveBeenCalled()
  })

  it('a refusal for an active workflow the preview did not flag: asks, then retries with the flag', async () => {
    aitaskApplyApi.preview.mockResolvedValue(preview())
    aitaskApplyApi.apply.mockRejectedValueOnce(new Error('workflow_active_needs_confirm: it is running')).mockResolvedValueOnce({ ok: true, applied: 1, workflowId: 'w1' })
    mount()
    await clickApply()
    await screen.findByText(/Applied 1 edits/)
    expect(aitaskApplyApi.apply.mock.calls).toEqual([['t1', false], ['t1', true]])
  })

  it('a normal apply never sends the allow flag', async () => {
    aitaskApplyApi.preview.mockResolvedValue(preview())
    aitaskApplyApi.apply.mockResolvedValue({ ok: true, applied: 1, workflowId: 'w1' })
    mount()
    await clickApply()
    await screen.findByText(/Applied 1 edits/)
    expect(aitaskApplyApi.apply).toHaveBeenCalledWith('t1', false)
  })
})
