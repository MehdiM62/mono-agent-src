// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, cleanup } from '@testing-library/react'

const { aitaskApi, aitaskBudgetApi } = vi.hoisted(() => ({
  aitaskApi: { get: vi.fn(), cancel: vi.fn() },
  aitaskBudgetApi: { forTask: vi.fn() },
}))
vi.mock('../SendToCoderApi.js', async (orig) => ({ ...(await orig()), aitaskApi }))
vi.mock('../settings/aitaskBudgetApi.js', async (orig) => ({ ...(await orig()), aitaskBudgetApi }))

import { TaskHeaderCard } from './TaskHeaderCard.jsx'

afterEach(() => { cleanup(); vi.clearAllMocks() })

function done(kind, result, onNavigate) {
  aitaskBudgetApi.forTask.mockResolvedValue(null)
  aitaskApi.get.mockResolvedValue({ status: 'complete', kind, result })
  render(<TaskHeaderCard task={{ taskId: 't1', kind, goal: 'g', status: 'complete' }} onNavigate={onNavigate} />)
  return screen.findByTestId('task-result')
}

describe('TaskHeaderCard result line', () => {
  it('workflow-generate: Created <name> (<n> nodes) and Open workflow', async () => {
    const onNavigate = vi.fn()
    const line = await done('workflow-generate', { workflowId: 'w1', name: 'Daily digest', summary: 's', nodeCount: 4, validation: { ok: true, errors: [] } }, onNavigate)
    expect(line).toHaveTextContent('Created Daily digest (4 nodes)')
    expect(screen.queryByText('needs fixes')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Open workflow' }))
    expect(onNavigate).toHaveBeenCalledWith('noderunner', { workflowId: 'w1' })
  })

  it('workflow-generate: a warning chip when validation failed', async () => {
    await done('workflow-generate', { workflowId: 'w1', name: 'X', nodeCount: 1, validation: { ok: false, errors: ['bad edge'] } }, vi.fn())
    expect(screen.getByText('needs fixes')).toBeInTheDocument()
  })

  it('recording-analyze: summary with saved and verified chips', async () => {
    const line = await done('recording-analyze', { recordingId: 'r', summary: 'Logs in', saved: true, verified: true })
    expect(line).toHaveTextContent('Logs in')
    expect(screen.getByText('saved')).toBeInTheDocument()
    expect(screen.getByText('verified')).toBeInTheDocument()
  })

  it('failed-run-fix: diagnosis, changes collapsed, needs approval chip', async () => {
    const line = await done('failed-run-fix', { diagnosis: 'Bad URL', changed: true, changes: ['set url'], needsApproval: true })
    expect(line).toHaveTextContent('Bad URL')
    expect(screen.getByText('needs approval')).toBeInTheDocument()
    expect(screen.queryByText('set url')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /Show 1 change/ }))
    expect(screen.getByText('set url')).toBeInTheDocument()
  })

  it('renders result text as text: no HTML, no links, capped length', async () => {
    const line = await done('recording-analyze', { summary: '<a href="http://evil.test">click</a> http://evil.test ' + 'y'.repeat(900) })
    expect(line.querySelector('a')).toBeNull()
    expect(line).toHaveTextContent('<a href="http://evil.test">click</a>')
    expect(line.textContent.length).toBeLessThan(450)
  })

  it('shows nothing for a task without a result', async () => {
    aitaskBudgetApi.forTask.mockResolvedValue(null)
    aitaskApi.get.mockResolvedValue({ status: 'complete', kind: 'generic' })
    render(<TaskHeaderCard task={{ taskId: 't1', kind: 'generic', goal: 'g', status: 'complete' }} />)
    await new Promise(r => setTimeout(r, 20))
    expect(screen.queryByTestId('task-result')).toBeNull()
  })
})
