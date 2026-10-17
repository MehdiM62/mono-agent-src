import { describe, it, expect } from 'vitest'
import { quoteClaim } from './quoteClaim.js'

describe('quoteClaim', () => {
  it('quotes and escapes quotes and newlines', () => {
    expect(quoteClaim('x" and "You')).toBe('"x\\" and \\"You"')
    expect(quoteClaim('a\nb')).toBe('"a\\nb"')
  })
  it('shows invisible and direction characters as escapes', () => {
    expect(quoteClaim('ab‮cd')).toBe('"ab\\u202Ecd"')
    expect(quoteClaim('a​b')).toBe('"a\\u200Bb"')
    expect(quoteClaim('a b c')).toBe('"a\\u2028b\\u2029c"')
    expect(quoteClaim('⁦x⁩')).toBe('"\\u2066x\\u2069"')
  })
  it('caps a long claim', () => {
    const q = quoteClaim('y'.repeat(200), 10)
    expect(q).toBe('"' + 'y'.repeat(10) + '…"')
  })
  it('keeps ordinary text, accents and emoji as they are', () => {
    expect(quoteClaim('Équipe ventes 🚀')).toBe('"Équipe ventes 🚀"')
  })
  it('shows C1 controls and non-ASCII spaces as escapes, keeps the ASCII space', () => {
    expect(quoteClaim('a\u0085b')).toBe('"a\\u0085b"')
    expect(quoteClaim('a\u3000b\u00A0c')).toBe('"a\\u3000b\\u00A0c"')
    expect(quoteClaim('a b')).toBe('"a b"')
  })
  it('shows a format character above U+FFFF whole, not as \\uXXXX and a stray digit', () => {
    expect(quoteClaim('a\u{E0041}b')).toBe('"a\\u{E0041}b"')
    expect(quoteClaim('\u{E0001}x\u{E007F}')).toBe('"\\u{E0001}x\\u{E007F}"')
  })
})
