// Every ask.* key AskCard and its host views use exists in English and
// Spanish, including the channel names built from an item's channel.
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import en from './en.json'
import es from './es.json'

const flat = (o, p = '') => Object.entries(o).flatMap(([k, v]) => (v && typeof v === 'object' ? flat(v, `${p}${k}.`) : [`${p}${k}`]))
const has = (keys, k) => keys.includes(k) || (keys.includes(`${k}_one`) && keys.includes(`${k}_other`))

const sources = ['bubbles/AskCard.jsx', 'bubbles/CoderChatView.jsx', 'bubbles/OrgChatView.jsx'].map(f => join(__dirname, '..', 'components', f))
const literal = [...new Set(sources.flatMap(f => [...readFileSync(f, 'utf8').matchAll(/['"`](ask\.[a-zA-Z0-9_.]+)['"`]/g)].map(m => m[1])))]
const dynamic = ['email', 'dm', 'comment', 'post', 'connection'].map(c => `ask.channel.${c}`)

describe('ask card i18n', () => {
  const enKeys = flat(en)
  const esKeys = flat(es)
  it('finds the keys the card uses', () => {
    expect(literal.length).toBeGreaterThan(20)
  })
  it('has every used key in English and Spanish', () => {
    const used = [...literal, ...dynamic]
    expect(used.filter(k => !has(enKeys, k))).toEqual([])
    expect(used.filter(k => !has(esKeys, k))).toEqual([])
  })
  it('carries the same ask keys in both locales', () => {
    const s = keys => keys.filter(k => k.startsWith('ask.')).sort()
    expect(s(esKeys)).toEqual(s(enKeys))
  })
})
