// Every coderQueue.* key the queued coder bubble uses exists in English and
// Spanish, and both locales carry the same coderQueue keys.
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import en from './en.json'
import es from './es.json'

const flat = (o, p = '') => Object.entries(o).flatMap(([k, v]) => (v && typeof v === 'object' ? flat(v, `${p}${k}.`) : [`${p}${k}`]))
// i18next plurals: "x_one"/"x_other" satisfy t('x', { count }).
const has = (keys, k) => keys.includes(k) || (keys.includes(`${k}_one`) && keys.includes(`${k}_other`))

const source = readFileSync(join(__dirname, '..', 'components', 'bubbles', 'QueuedCoderBubble.jsx'), 'utf8')
const literal = [...new Set([...source.matchAll(/['"`](coderQueue\.[a-zA-Z0-9_.-]+)['"`]/g)].map(m => m[1]))]
// The kinds `coder queue add --kind` takes (t(`coderQueue.kind.${kind}`)).
const kinds = ['copy-style', 'tokens', 'fix-element', 'bug-report', 'integration', 'ask'].map(k => `coderQueue.kind.${k}`)
const used = [...literal, ...kinds]

describe('coderQueue i18n', () => {
  const enKeys = flat(en)
  const esKeys = flat(es)
  it('finds the keys the queued bubble uses', () => {
    expect(literal.length).toBeGreaterThan(10)
  })
  it('has every used key in English and Spanish', () => {
    expect(used.filter(k => !has(enKeys, k))).toEqual([])
    expect(used.filter(k => !has(esKeys, k))).toEqual([])
  })
  it('carries the same coderQueue keys in both locales', () => {
    const q = keys => keys.filter(k => k.startsWith('coderQueue.')).sort()
    expect(q(esKeys)).toEqual(q(enKeys))
  })
})
