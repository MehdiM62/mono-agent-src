// @vitest-environment jsdom
// The board's ghost drag (spec §10, Interaction): no native drag and drop,
// which the WebKit view does not run reliably. jsdom has no layout, so the
// card boxes and the hit test are set by hand.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '../i18n.js'

const { api } = vi.hoisted(() => ({
  api: { board: vi.fn(), show: vi.fn(), add: vi.fn(), edit: vi.fn(), move: vi.fn(), approve: vi.fn(), archive: vi.fn(), comment: vi.fn(), agentShell: vi.fn() },
}))
vi.mock('../services/tasks.js', () => ({ tasksApi: api, onTasksChanged: () => () => {} }))
import Tasks from './Tasks.jsx'

const card = (id, extra = {}) => ({ id, title: `Task ${id}`, notes: '', position: id, claim: null, ...extra })
const doc = () => ({ rev: 1, counts: {}, tasks: { inbox: [card(1), card(2, { notes: 'needle' })], ready: [card(3), card(4)] } })

const cardEl = (id) => document.querySelector(`[data-task-id="${id}"]`)
const colEl = (status) => document.querySelector(`[data-tb-column="${status}"]`)
// Ready's cards: #3 from 40 to 80 (middle 60), #4 from 90 to 130 (middle 110).
function layout() {
  for (const [id, top] of [[3, 40], [4, 90]]) {
    cardEl(id).getBoundingClientRect = () => ({ top, height: 40, bottom: top + 40, left: 300, width: 200, right: 500 })
  }
}
let over = 'ready'
let frames = []
const runFrame = () => act(() => { const due = frames; frames = []; due.forEach(f => f()) })

beforeEach(() => {
  Object.values(api).forEach(f => f.mockReset())
  api.board.mockResolvedValue(doc())
  api.agentShell.mockResolvedValue('')
  api.show.mockResolvedValue({ task: card(1), events: [] })
  api.move.mockResolvedValue({ id: 1 })
  over = 'ready'
  frames = []
  document.elementFromPoint = () => colEl(over)
  vi.stubGlobal('requestAnimationFrame', (f) => frames.push(f))
  vi.stubGlobal('cancelAnimationFrame', () => {})
})
afterEach(() => {
  cleanup()
  delete document.elementFromPoint
  vi.unstubAllGlobals()
})

async function board() {
  render(<Tasks />)
  await screen.findByText('Task 1')
  await waitFor(() => expect(screen.getAllByLabelText('New task title')).not.toHaveLength(0)) // writable
  layout()
}

describe('the ghost drag', () => {
  it('drags past the threshold with a ghost, a glowing column and a drop line, and drops between two cards', async () => {
    await board()
    fireEvent.mouseDown(cardEl(1), { button: 0, clientX: 10, clientY: 10 })
    fireEvent.mouseMove(document, { clientX: 12, clientY: 12 }) // under the threshold
    expect(screen.queryByTestId('tb-ghost')).toBeNull()
    fireEvent.mouseMove(document, { clientX: 320, clientY: 95 })
    expect(screen.getByTestId('tb-ghost')).toHaveTextContent('Task 1')
    expect(colEl('ready')).toHaveAttribute('data-drop-target', 'true')
    expect(colEl('inbox')).not.toHaveAttribute('data-drop-target')
    expect(cardEl(1).className).toContain('tb-card--dragging')
    expect(screen.getByTestId('tb-drop-line').nextElementSibling).toBe(cardEl(4))
    expect(document.body).toHaveClass('tb-dragging')
    fireEvent.mouseUp(document, { clientX: 320, clientY: 95 })
    fireEvent.click(cardEl(1).querySelector('[data-card-open]')) // the click that ends the drag
    await waitFor(() => expect(api.move).toHaveBeenCalledWith(1, 'ready', { where: 'before', ref: 4 }))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(screen.queryByTestId('tb-ghost')).toBeNull()
    expect(screen.queryByTestId('tb-drop-line')).toBeNull()
    expect(document.body).not.toHaveClass('tb-dragging')
    expect(screen.getByTestId('tb-live')).toHaveTextContent('Moved “Task 1” to Ready')
  })

  it('drops below the last card as right after it, and from the title too', async () => {
    await board()
    fireEvent.mouseDown(cardEl(1).querySelector('[data-card-open]'), { button: 0, clientX: 10, clientY: 10 })
    fireEvent.mouseMove(document, { clientX: 320, clientY: 200 })
    expect(screen.getByTestId('tb-drop-line').previousElementSibling).toBe(cardEl(4))
    fireEvent.mouseUp(document, { clientX: 320, clientY: 200 })
    await waitFor(() => expect(api.move).toHaveBeenCalledWith(1, 'ready', { where: 'after', ref: 4 }))
  })

  it('opens on a plain click, cancels with Escape or a lost window, never drags from a button, ignores a drop in place', async () => {
    await board()
    fireEvent.mouseDown(cardEl(1), { button: 0, clientX: 10, clientY: 10 })
    fireEvent.mouseUp(document, { clientX: 10, clientY: 10 })
    fireEvent.click(cardEl(1))
    expect(await screen.findByRole('dialog')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))

    fireEvent.mouseDown(cardEl(1), { button: 0, clientX: 10, clientY: 10 })
    fireEvent.mouseMove(document, { clientX: 320, clientY: 95 })
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByTestId('tb-ghost')).toBeNull()
    fireEvent.mouseUp(document, { clientX: 320, clientY: 95 })

    fireEvent.mouseDown(cardEl(1), { button: 0, clientX: 10, clientY: 10 })
    fireEvent.mouseMove(document, { clientX: 320, clientY: 95 })
    fireEvent.blur(window)
    expect(screen.queryByTestId('tb-ghost')).toBeNull()
    fireEvent.mouseUp(document, { clientX: 320, clientY: 95 })

    fireEvent.mouseDown(screen.getByRole('button', { name: 'Archive: Task 1' }), { button: 0, clientX: 10, clientY: 10 })
    fireEvent.mouseMove(document, { clientX: 320, clientY: 95 })
    expect(screen.queryByTestId('tb-ghost')).toBeNull()
    fireEvent.mouseUp(document, { clientX: 320, clientY: 95 })

    fireEvent.mouseDown(cardEl(3), { button: 0, clientX: 310, clientY: 50 })
    fireEvent.mouseMove(document, { clientX: 320, clientY: 55 }) // a drag that ends where #3 already is
    fireEvent.mouseUp(document, { clientX: 320, clientY: 55 })
    expect(api.move).not.toHaveBeenCalled()
    expect(api.archive).not.toHaveBeenCalled()
  })

  it('opens a card with notes dropped on Ready to be read, moving nothing (plan ruling F6)', async () => {
    await board()
    fireEvent.mouseDown(cardEl(2), { button: 0, clientX: 10, clientY: 10 })
    fireEvent.mouseMove(document, { clientX: 320, clientY: 95 })
    fireEvent.mouseUp(document, { clientX: 320, clientY: 95 })
    expect(await screen.findByTestId('read-first')).toBeInTheDocument()
    expect(api.move).not.toHaveBeenCalled()
    expect(api.approve).not.toHaveBeenCalled()
    expect(within(colEl('inbox')).getByText('Task 2')).toBeInTheDocument()
  })

  it('never drags on a read-only board', async () => {
    api.agentShell.mockResolvedValue('monomind')
    render(<Tasks />)
    await screen.findByText(/Read-only/)
    fireEvent.mouseDown(cardEl(1), { button: 0, clientX: 10, clientY: 10 })
    fireEvent.mouseMove(document, { clientX: 320, clientY: 95 })
    expect(screen.queryByTestId('tb-ghost')).toBeNull()
    fireEvent.mouseUp(document, { clientX: 320, clientY: 95 })
    expect(api.move).not.toHaveBeenCalled()
  })

  it('scrolls the column and the board near their edges while dragging', async () => {
    await board()
    const col = colEl('ready')
    const cols = document.querySelector('[data-tb-board-scroll]')
    col.getBoundingClientRect = () => ({ top: 0, bottom: 300, left: 300, right: 500, height: 300, width: 200 })
    cols.getBoundingClientRect = () => ({ top: 0, bottom: 400, left: 0, right: 1000, height: 400, width: 1000 })
    Object.defineProperty(col, 'scrollTop', { value: 0, writable: true })
    Object.defineProperty(cols, 'scrollLeft', { value: 0, writable: true })
    fireEvent.mouseDown(cardEl(1), { button: 0, clientX: 10, clientY: 10 })
    fireEvent.mouseMove(document, { clientX: 990, clientY: 290 }) // the column's bottom edge, the board's right edge
    runFrame()
    expect(col.scrollTop).toBeGreaterThan(0)
    expect(cols.scrollLeft).toBeGreaterThan(0)
    fireEvent.mouseUp(document, { clientX: 990, clientY: 290 })
    runFrame()
    expect(frames).toEqual([]) // the drag ended: no more scrolling
  })
})
