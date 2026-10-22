// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, waitFor, cleanup } from '@testing-library/react'

const { aitaskApi, aitaskBudgetApi } = vi.hoisted(() => ({
  aitaskApi: { get: vi.fn(), cancel: vi.fn() },
  aitaskBudgetApi: { forTask: vi.fn() },
}))
vi.mock('../SendToCoderApi.js', async (orig) => ({ ...(await orig()), aitaskApi }))
vi.mock('../settings/aitaskBudgetApi.js', async (orig) => ({ ...(await orig()), aitaskBudgetApi }))

import { TaskHeaderCard } from './TaskHeaderCard.jsx'

const task = { taskId: 't1', goal: 'Tidy the notes', status: 'running' }

beforeEach(() => { aitaskApi.get.mockResolvedValue({ status: 'running' }) })
afterEach(() => { cleanup(); vi.clearAllMocks() })

describe('TaskHeaderCard budget', () => {
  it('shows what the task has spent of its cap', async () => {
    aitaskBudgetApi.forTask.mockResolvedValue({ taskId: 't1', capUsd: 2, spentUsd: 0.5, reported: true })
    render(<TaskHeaderCard task={task} />)
    expect(await screen.findByTestId('task-budget')).toHaveTextContent('$0.5000 / $2.00')
  })

  it('shows a dash, not $0, while no cost has been reported', async () => {
    aitaskBudgetApi.forTask.mockResolvedValue({ taskId: 't1', capUsd: 2, spentUsd: 0, reported: false })
    render(<TaskHeaderCard task={task} />)
    expect(await screen.findByTestId('task-budget')).toHaveTextContent('– / $2.00')
  })

  it('shows nothing when the CLI cannot say', async () => {
    aitaskBudgetApi.forTask.mockRejectedValue(new Error('no cli'))
    render(<TaskHeaderCard task={task} />)
    await waitFor(() => expect(aitaskBudgetApi.forTask).toHaveBeenCalledWith('t1'))
    expect(screen.queryByTestId('task-budget')).toBeNull()
    expect(screen.getByTestId('task-header')).toBeInTheDocument()
  })
})
