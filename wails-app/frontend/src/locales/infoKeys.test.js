// Every info.* key the Info page uses exists in English and Spanish, and
// both locales carry the same keys.
import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import en from './en.json'
import es from './es.json'

const flat = (o, p = '') => Object.entries(o).flatMap(([k, v]) => (v && typeof v === 'object' ? flat(v, `${p}${k}.`) : [`${p}${k}`]))
const has = (keys, k) => keys.includes(k) || (keys.includes(`${k}_one`) && keys.includes(`${k}_other`))
const src = join(__dirname, '..')
const files = [join(src, 'pages', 'Info.jsx'), ...readdirSync(join(src, 'components', 'info')).filter(f => f.endsWith('.jsx')).map(f => join(src, 'components', 'info', f))]
const used = [...new Set(files.flatMap(f => [...readFileSync(f, 'utf8').matchAll(/['"`](info\.[a-zA-Z0-9_.]+)['"`]/g)].map(m => m[1])))]

describe('info locale keys', () => {
  it('finds the keys the page uses', () => expect(used.length).toBeGreaterThan(10))
  it.each(['en', 'es'])('%s has every used key', (lang) => {
    const keys = flat(lang === 'en' ? en : es)
    expect(used.filter(k => !has(keys, k))).toEqual([])
  })
  it('both locales have the same info keys and the sidebar label', () => {
    const pick = (o) => flat(o).filter(k => k.startsWith('info.') || k === 'sidebar.nav.info').sort()
    expect(pick(es)).toEqual(pick(en))
    expect(pick(en)).toContain('sidebar.nav.info')
  })
})
