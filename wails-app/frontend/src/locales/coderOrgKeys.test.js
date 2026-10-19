// Every coderOrg.* key the coder-org settings and the send card use exists in
// English and Spanish, and both locales carry the same coderOrg keys.
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import en from './en.json'
import es from './es.json'
import { ORG_ROUTERS, SEND_ACTIONS } from '../components/settings/CoderOrgSection.jsx'

const flat = (o, p = '') => Object.entries(o).flatMap(([k, v]) => (v && typeof v === 'object' ? flat(v, `${p}${k}.`) : [`${p}${k}`]))

const files = [['components', 'settings', 'CoderOrgSection.jsx'], ['components', 'bubbles', 'AskCard.jsx'], ['pages', 'Settings.jsx']]
const source = files.map(f => readFileSync(join(__dirname, '..', ...f), 'utf8')).join('\n')
const literal = [...new Set([...source.matchAll(/['"`](coderOrg\.[a-zA-Z0-9_.-]+)['"`]/g)].map(m => m[1]))]
// t(`coderOrg.router.${r}`), t(`coderOrg.routerDesc.${r}`), t(`coderOrg.action.${a}`).
const dynamic = [
  ...ORG_ROUTERS.flatMap(r => [`coderOrg.router.${r}`, `coderOrg.routerDesc.${r}`]),
  ...SEND_ACTIONS.map(a => `coderOrg.action.${a}`),
]
const used = [...literal, ...dynamic]

describe('coderOrg i18n', () => {
  const enKeys = flat(en)
  const esKeys = flat(es)
  it('finds the keys the section and the card use', () => {
    expect(literal.length).toBeGreaterThan(20)
    expect(literal).toContain('coderOrg.alwaysAllow')
  })
  it('has every used key in English and Spanish', () => {
    expect(used.filter(k => !enKeys.includes(k))).toEqual([])
    expect(used.filter(k => !esKeys.includes(k))).toEqual([])
  })
  it('carries the same coderOrg keys in both locales', () => {
    const q = keys => keys.filter(k => k.startsWith('coderOrg.')).sort()
    expect(q(esKeys)).toEqual(q(enKeys))
  })
})
