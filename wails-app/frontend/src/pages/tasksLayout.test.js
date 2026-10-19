// The Tasks board stays readable with the drawer open (#365 verification):
// the columns keep their width and the board scrolls sideways instead of
// squeezing the Ready quick add to an empty box, and a long page host on a
// card is cut with an ellipsis instead of running past the card's edge.
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

const css = readFileSync(join(__dirname, '..', 'index.css'), 'utf8')
const rule = (selector) => {
  const at = css.indexOf(`${selector} {`)
  expect(at, `${selector} has a rule`).toBeGreaterThanOrEqual(0)
  return css.slice(at, css.indexOf('}', at))
}
const px = (decl, prop) => Number((decl.match(new RegExp(`(?:^|[;{\\s])${prop}:\\s*(\\d+)px`)) || [])[1] || 0)

describe('the Tasks board layout', () => {
  it('keeps each column at least 200 px and scrolls the board sideways', () => {
    expect(px(rule('.tb-col'), 'min-width')).toBeGreaterThanOrEqual(200)
    expect(rule('.tb-cols')).toMatch(/overflow-x:\s*auto/)
  })

  it('keeps the quick-add field usable: it wraps the button under it rather than shrink to nothing', () => {
    expect(rule('.tb-quick')).toMatch(/flex-wrap:\s*wrap/)
    expect(px(rule('.tb-quick .form-input'), 'min-width')).toBeGreaterThanOrEqual(100)
  })

  it('cuts a long source chip with an ellipsis inside the card', () => {
    expect(rule('.tb-chip')).toMatch(/max-width:\s*100%/)
    const text = rule('.tb-chip-text')
    expect(text).toMatch(/text-overflow:\s*ellipsis/)
    expect(text).toMatch(/white-space:\s*nowrap/)
    expect(text).toMatch(/overflow:\s*hidden/)
  })
})
