// Every toolLinks.* key the tool cards use exists in English and Spanish, and
// both locales carry the same toolLinks keys.
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import en from './en.json'
import es from './es.json'
import { TOOL_LINK_PAGES } from '../components/chat/toolLinks.js'

const flat = (o, p = '') => Object.entries(o).flatMap(([k, v]) => (v && typeof v === 'object' ? flat(v, `${p}${k}.`) : [`${p}${k}`]))

const files = ['ToolLink.jsx', 'nativeToolDescribe.jsx', 'toolLinks.js']
const source = files.map(f => readFileSync(join(__dirname, '..', 'components', 'chat', f), 'utf8')).join('\n')
const literal = [...new Set([...source.matchAll(/['"`](toolLinks\.[a-zA-Z0-9_.-]+)['"`]/g)].map(m => m[1]))]
// t(`toolLinks.page.${page}`) for every tab a card can open.
const used = [...literal, ...Object.values(TOOL_LINK_PAGES).map(p => `toolLinks.page.${p}`)]

describe('toolLinks i18n', () => {
  const enKeys = flat(en)
  const esKeys = flat(es)
  it('finds the keys the cards use', () => {
    expect(literal).toEqual(expect.arrayContaining(['toolLinks.open', 'toolLinks.command', 'toolLinks.vaultHidden']))
  })
  it('has every used key in English and Spanish', () => {
    expect(used.filter(k => !enKeys.includes(k))).toEqual([])
    expect(used.filter(k => !esKeys.includes(k))).toEqual([])
  })
  it('carries the same toolLinks keys in both locales', () => {
    const q = keys => keys.filter(k => k.startsWith('toolLinks.')).sort()
    expect(q(esKeys)).toEqual(q(enKeys))
  })
})
