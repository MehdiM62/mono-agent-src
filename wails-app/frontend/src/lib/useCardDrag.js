import { useCallback, useEffect, useRef, useState } from 'react'
import { dropIndex } from './taskModel.js'

// A press becomes a drag once the mouse has moved this far; a shorter press
// is a click and opens the card.
export const DRAG_THRESHOLD = 5
const EDGE = 48 // px from a column's or the board's edge where scrolling starts
const MAX_SPEED = 16 // px per frame at the very edge

const frame = (fn) => (typeof requestAnimationFrame === 'function' ? requestAnimationFrame(fn) : setTimeout(fn, 16))
const cancelFrame = (id) => (typeof cancelAnimationFrame === 'function' ? cancelAnimationFrame(id) : clearTimeout(id))
const speed = (depth) => Math.min(MAX_SPEED, Math.ceil((depth / EDGE) * MAX_SPEED))
const cardsIn = (col, without) => [...col.querySelectorAll('[data-task-id]')].filter(el => Number(el.dataset.taskId) !== without)

// useCardDrag is the app's mouse ghost drag (no native drag and drop, which
// this WebKit view does not run reliably: see OrgDesigner.jsx). A press on a
// card and a move past DRAG_THRESHOLD start it; the card then follows the
// mouse as a ghost, the column under it glows, a line marks where it will
// land, and near a column's or the board's edge that one scrolls. Escape,
// the window losing focus or a button let go outside it cancels, and the
// click that ends the press then opens nothing; a press on a card's buttons
// (but its title), a link or a field never starts one. Columns carry
// data-tb-column={status}, cards data-task-id, the sideways scroller
// data-tb-board-scroll. On release over a column onDrop gets {id, from,
// fromIds, to, ids, index}: ids are the column's shown cards without the
// dragged one, top to bottom. A column's cards are measured once per drag,
// and again, with the landing place, after a scroll, a resize or a new
// layoutKey (the shown board).
// elementFromPoint is injectable because jsdom has no layout.
export function useCardDrag({ rootRef, onDrop, canDrag, elementFromPoint, layoutKey }) {
  const [drag, setDrag] = useState(null)
  const gesture = useRef(null)
  const listeners = useRef(null)
  const scrollFrame = useRef(0)
  const clickGuard = useRef(false)
  const disarm = useRef(null)
  const opts = useRef({})
  opts.current = { onDrop, canDrag, hit: elementFromPoint || ((x, y) => document.elementFromPoint(x, y)) }

  // locate reads where the dragged card would land at the pointer: the
  // column under it and the place among that column's cards (measured once,
  // and again after the layout changes).
  const locate = useCallback(() => {
    const g = gesture.current
    if (!g?.started) return
    const col = opts.current.hit(g.x, g.y)?.closest?.('[data-tb-column]') || null
    g.over = col?.dataset.tbColumn || null
    g.ids = []
    g.index = 0
    if (col) {
      let m = g.cache.get(col)
      if (!m) {
        const els = cardsIn(col, g.id)
        m = { ids: els.map(el => Number(el.dataset.taskId)), rects: els.map(el => el.getBoundingClientRect()) }
        g.cache.set(col, m)
      }
      g.ids = m.ids
      g.index = dropIndex(m.rects, g.y)
    }
    setDrag({ id: g.id, x: g.x, y: g.y, offsetX: g.offsetX, offsetY: g.offsetY, width: g.width, overStatus: g.over, overIndex: g.index })
  }, [])
  // A new board, a scroll or a resize moves the boxes: measure again and
  // take the landing place afresh.
  const relayout = useCallback(() => {
    gesture.current?.cache.clear()
    locate()
  }, [locate])
  useEffect(() => { relayout() }, [layoutKey, relayout])

  // guardClick swallows the click that ends a drag's press, whenever it
  // comes: right after a drop, or when the button is let go after Escape or
  // a lost window. The next press or key clears it, so a button released
  // outside the window eats no later click.
  const guardClick = useCallback(() => {
    disarm.current?.()
    clickGuard.current = true
    const clear = () => { clickGuard.current = false; off() }
    const off = () => {
      document.removeEventListener('mousedown', clear, true)
      document.removeEventListener('keydown', clear, true)
      disarm.current = null
    }
    document.addEventListener('mousedown', clear, true)
    document.addEventListener('keydown', clear, true)
    disarm.current = off
  }, [])

  const end = useCallback(() => {
    const l = listeners.current
    if (l) {
      document.removeEventListener('mousemove', l.move)
      document.removeEventListener('mouseup', l.up)
      document.removeEventListener('keydown', l.key, true)
      document.removeEventListener('scroll', l.scrolled, true)
      window.removeEventListener('resize', l.scrolled)
      window.removeEventListener('blur', l.cancel)
      listeners.current = null
    }
    if (gesture.current?.started) guardClick()
    if (scrollFrame.current) cancelFrame(scrollFrame.current)
    scrollFrame.current = 0
    document.body.classList.remove('tb-dragging')
    gesture.current = null
    setDrag(null)
  }, [guardClick])
  // Unmounting leaves no listener behind.
  useEffect(() => () => { end(); disarm.current?.() }, [end])

  const autoScroll = useCallback(() => {
    const g = gesture.current
    if (!g?.started) {
      scrollFrame.current = 0
      return
    }
    const col = opts.current.hit(g.x, g.y)?.closest?.('[data-tb-column]')
    if (col) {
      const r = col.getBoundingClientRect()
      if (g.y < r.top + EDGE) col.scrollTop -= speed(r.top + EDGE - g.y)
      else if (g.y > r.bottom - EDGE) col.scrollTop += speed(g.y - (r.bottom - EDGE))
    }
    const board = rootRef.current?.querySelector?.('[data-tb-board-scroll]')
    if (board) {
      const r = board.getBoundingClientRect()
      if (g.x < r.left + EDGE) board.scrollLeft -= speed(r.left + EDGE - g.x)
      else if (g.x > r.right - EDGE) board.scrollLeft += speed(g.x - (r.right - EDGE))
    }
    scrollFrame.current = frame(autoScroll)
  }, [rootRef])

  const onCardMouseDown = useCallback((e, id, status) => {
    if (e.button !== 0 || listeners.current) return
    if (e.target.closest?.('button:not([data-card-open]), a, input, textarea, select')) return
    if (!opts.current.canDrag(id)) return
    const card = e.currentTarget
    const rect = card.getBoundingClientRect()
    const fromCol = card.closest('[data-tb-column]')
    gesture.current = {
      id, from: status, fromIds: fromCol ? cardsIn(fromCol, -1).map(el => Number(el.dataset.taskId)) : [],
      startX: e.clientX, startY: e.clientY, x: e.clientX, y: e.clientY,
      offsetX: e.clientX - rect.left, offsetY: e.clientY - rect.top, width: rect.width,
      started: false, over: null, index: 0, ids: [], cache: new Map(),
    }
    const move = (ev) => {
      const g = gesture.current
      if (!g) return
      // No button down: it was let go outside the window, where no mouseup
      // reaches the page. The drag ends with no drop.
      if (ev.buttons === 0) {
        end()
        return
      }
      g.x = ev.clientX
      g.y = ev.clientY
      if (!g.started) {
        if (Math.hypot(g.x - g.startX, g.y - g.startY) < DRAG_THRESHOLD) return
        g.started = true
        document.body.classList.add('tb-dragging')
        if (!scrollFrame.current) scrollFrame.current = frame(autoScroll)
      }
      locate()
    }
    const up = () => {
      const g = gesture.current
      const drop = g?.started && g.over ? { id: g.id, from: g.from, fromIds: g.fromIds, to: g.over, ids: g.ids, index: g.index } : null
      end()
      if (drop) opts.current.onDrop(drop)
    }
    const key = (ev) => {
      if (ev.key !== 'Escape') return
      ev.preventDefault()
      ev.stopPropagation()
      end()
    }
    listeners.current = { move, up, key, scrolled: relayout, cancel: end }
    document.addEventListener('mousemove', move)
    document.addEventListener('mouseup', up)
    document.addEventListener('keydown', key, true)
    document.addEventListener('scroll', relayout, true)
    window.addEventListener('resize', relayout)
    window.addEventListener('blur', end)
  }, [autoScroll, end, locate, relayout])

  // consumeClick says, once, that the click now arriving ends a drag.
  const consumeClick = useCallback(() => {
    const was = clickGuard.current
    clickGuard.current = false
    disarm.current?.()
    return was
  }, [])

  return { drag, onCardMouseDown, consumeClick }
}
