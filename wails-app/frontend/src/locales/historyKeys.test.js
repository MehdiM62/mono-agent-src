// Every history.* key the history drawer uses exists in English and Spanish,
// and both locales carry the same history keys.
import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import en from './en.json'
import es from './es.json'

const flat = (o, p = '') => Object.entries(o).flatMap(([k, v]) => (v && typeof v === 'object' ? flat(v, `${p}${k}.`) : [`${p}${k}`]))
const has = (keys, k) => keys.includes(k) || (keys.includes(`${k}_one`) && keys.includes(`${k}_other`))

const dir = join(__dirname, '..', 'components', 'history')
const sources = readdirSync(dir).filter(f => /\.jsx?$/.test(f) && !f.includes('.test.')).map(f => join(dir, f))
// t('history.x') and the dynamic t(`history.status_${...}`) / access_ families.
const used = [...new Set(sources.flatMap(f => [...readFileSync(f, 'utf8').matchAll(/['"`](history\.[a-zA-Z0-9_]+)['"`]/g)].map(m => m[1])))]
const families = ['history.status_running', 'history.status_needs', 'history.status_error', 'history.status_done', 'history.status_idle',
  'history.access_scoped', 'history.access_coding', 'history.access_workspace', 'history.access_full']

describe('history i18n', () => {
  const enKeys = flat(en), esKeys = flat(es)
  it('finds the keys the drawer uses', () => { expect(used.length).toBeGreaterThan(25) })
  it('has every used key in English and Spanish', () => {
    expect([...used, ...families].filter(k => !has(enKeys, k))).toEqual([])
    expect([...used, ...families].filter(k => !has(esKeys, k))).toEqual([])
  })
  it('carries the same history keys in both locales', () => {
    const h = keys => keys.filter(k => k.startsWith('history.') || k === 'sidebar.nav.history').sort()
    expect(h(esKeys)).toEqual(h(enKeys))
  })
})
