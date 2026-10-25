// The session tab strip scrolls sideways (overflow-x: auto), which also clips
// vertically: a focus ring (2px outline + 2px offset) and the selected tab's
// glow need room on top and at the sides inside the strip, or they are cut off.
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const css = readFileSync(fileURLToPath(new URL('./index.css', import.meta.url)), 'utf8')

describe('.ts-tabs', () => {
  it('pads the strip so a focused tab keeps its whole ring', () => {
    const rule = css.match(/\.ts-tabs\s*\{([^}]*)\}/)?.[1] || ''
    const pad = rule.match(/padding:\s*(\d+)px(?:\s+(\d+)px)?/)
    expect(pad, 'a padding on .ts-tabs').toBeTruthy()
    expect(Number(pad[1])).toBeGreaterThanOrEqual(4)
    expect(Number(pad[2] ?? pad[1])).toBeGreaterThanOrEqual(4)
  })
})
