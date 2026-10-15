// @vitest-environment jsdom
import { describe, it, expect } from 'vitest'
import { imagePathsInResult, takeFiles, formatBytes, MAX_ATTACHMENTS, MAX_ATTACHMENT_BYTES } from './chatAttachments.js'

describe('imagePathsInResult', () => {
  it('reads the path of a `browser screenshot --json` result', () => {
    const out = JSON.stringify({ path: '/w/app/.monoagent/screenshots/20261015-101500-1.png', width: 1280, height: 800 })
    expect(imagePathsInResult(out)).toEqual(['/w/app/.monoagent/screenshots/20261015-101500-1.png'])
  })

  it('reads nested JSON values, relative ones too', () => {
    const out = JSON.stringify({ shots: [{ width: 375, path: 'shots/a.webp' }, { width: 768, path: 'shots/b.JPG' }], note: 'x.txt' })
    expect(imagePathsInResult(out)).toEqual(['shots/a.webp', 'shots/b.JPG'])
  })

  it('reads absolute paths in plain text, but not URLs or bare names', () => {
    const out = 'Saved /w/app/out/home.png (1280x800)\nsee https://example.com/logo.png and logo.png\nC:\\work\\shot.jpeg done'
    expect(imagePathsInResult(out)).toEqual(['/w/app/out/home.png', 'C:\\work\\shot.jpeg'])
  })

  it('stops at four, skips repeats and ignores other files', () => {
    const out = Array.from({ length: 6 }, (_, i) => `/w/s${i}.png`).concat('/w/s0.png').join('\n')
    expect(imagePathsInResult(out)).toEqual(['/w/s0.png', '/w/s1.png', '/w/s2.png', '/w/s3.png'])
    expect(imagePathsInResult('/w/notes.txt /w/a.pngx')).toEqual([])
    expect(imagePathsInResult(null)).toEqual([])
  })
})

describe('takeFiles', () => {
  it('reads files into attachments with their base64, and names a pasted image', async () => {
    const files = [new File(['hello'], 'notes.txt', { type: 'text/plain' }), new File([new Uint8Array([1, 2, 3])], '', { type: 'image/png' })]
    const { added, skipped } = await takeFiles(0, files)
    expect(skipped).toEqual([])
    expect(added.map(a => [a.name, a.mime, a.size, a.base64])).toEqual([
      ['notes.txt', 'text/plain', 5, btoa('hello')],
      ['pasted.png', 'image/png', 3, btoa('\x01\x02\x03')],
    ])
    expect(added[1].dataUrl).toBe(`data:image/png;base64,${btoa('\x01\x02\x03')}`)
    expect(added[0].id).not.toBe(added[1].id)
  })

  it('leaves out files over the limits and says why', async () => {
    const big = new File(['x'], 'big.bin')
    Object.defineProperty(big, 'size', { value: MAX_ATTACHMENT_BYTES + 1 })
    const { added, skipped } = await takeFiles(MAX_ATTACHMENTS - 1, [new File(['a'], 'a.txt'), new File(['b'], 'b.txt'), big])
    expect(added.map(a => a.name)).toEqual(['a.txt'])
    expect(skipped).toEqual([`b.txt: at most ${MAX_ATTACHMENTS} files per message`, `big.bin: at most ${MAX_ATTACHMENTS} files per message`])
    const second = await takeFiles(0, [big])
    expect(second.skipped).toEqual(['big.bin: over 20 MB'])
  })
})

it('formatBytes', () => {
  expect([formatBytes(512), formatBytes(3 * 1024), formatBytes(5.5 * 1024 * 1024)]).toEqual(['512 B', '3 KB', '5.5 MB'])
})
