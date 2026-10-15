import { describe, it, expect } from 'vitest'
import {
  credentialPickerMode, groupConnectionsByPlatform, connectionLabel, fieldIsVisible,
  collectionText, commitCollection, jsonTextError, curlImportConfig, derivePlatformId,
} from './nodeInspectorRules.js'

// Field lists shaped like the CLI's `node palette` / `node schema` output.
const httpRequest = {
  credential_platform: null,
  fields: [
    { key: 'credential_id', label: 'Saved connection', type: 'credential_picker' },
    { key: 'url', type: 'text', required: true },
    { key: 'method', type: 'select', default: 'GET', options: ['GET', 'POST'] },
    { key: 'auth_type', type: 'select', default: 'none', options: ['none', 'credential', 'bearer'] },
    { key: 'credential_auth', type: 'text', depends_on: { key: 'auth_type', values: ['none', 'credential'] } },
    { key: 'body', type: 'textarea', depends_on: { key: 'method', values: ['POST', 'PUT', 'PATCH'] } },
    { key: 'follow_redirects', type: 'boolean', default: true },
    { key: 'max_redirects', type: 'number', default: 10, depends_on: { key: 'follow_redirects', values: ['true'] } },
  ],
}

describe('credential picker mode', () => {
  it('lists every platform when a credential_picker field names none', () => {
    const p = derivePlatformId({ subtype: 'http.request' }, httpRequest, new Set())
    expect(p).toBe(null)
    expect(credentialPickerMode(p, httpRequest)).toBe('any')
    expect(credentialPickerMode(null, { credential_platform: null, fields: [{ key: 'query', type: 'code' }] })).toBe(null)
  })
  it('keeps the platform filter where the schema declares one', () => {
    const schema = { credential_platform: 'calendly', fields: [{ key: 'credential_id', type: 'credential_picker' }] }
    expect(credentialPickerMode(derivePlatformId({ subtype: 'app.calendly' }, schema, new Set()), schema)).toBe('platform')
    expect(credentialPickerMode(derivePlatformId({ subtype: 'service.github' }, { fields: [] }, new Set()), { fields: [] })).toBe('platform')
  })
  it('groups connections by platform, sorted', () => {
    const groups = groupConnectionsByPlatform([
      { id: '3', platform: 'stripe', label: 'Live' },
      { id: '1', platform: 'github', label: 'work' },
      { id: '2', platform: 'github', label: 'personal' },
      { id: '4', label: 'no platform' },
    ])
    expect(groups.map(g => g.platform)).toEqual(['github', 'other', 'stripe'])
    expect(groups[0].connections.map(c => c.id)).toEqual(['2', '1'])
    expect(groupConnectionsByPlatform(null)).toEqual([])
  })
  it('labels a connection by its label, then account', () => {
    expect(connectionLabel({ id: 'x', label: 'L' })).toBe('L')
    expect(connectionLabel({ id: 'x', account_id: 'acc' })).toBe('acc')
    expect(connectionLabel({ id: 'x' })).toBe('x')
  })
})

describe('field visibility', () => {
  const f = Object.fromEntries(httpRequest.fields.map(x => [x.key, x]))
  it('keeps depends_on', () => {
    expect(fieldIsVisible(f.body, { method: 'POST' }, httpRequest.fields)).toBe(true)
    expect(fieldIsVisible(f.body, { method: 'GET' }, httpRequest.fields)).toBe(false)
    expect(fieldIsVisible(f.url, {}, httpRequest.fields)).toBe(true)
  })
  it('reads an unset key as its field default', () => {
    expect(fieldIsVisible(f.credential_auth, {}, httpRequest.fields)).toBe(true)
    expect(fieldIsVisible(f.max_redirects, {}, httpRequest.fields)).toBe(true)
    expect(fieldIsVisible(f.max_redirects, { follow_redirects: false }, httpRequest.fields)).toBe(false)
    expect(fieldIsVisible(f.body, {}, httpRequest.fields)).toBe(false)
  })
  it('accepts the legacy depends_on.field spelling', () => {
    expect(fieldIsVisible({ key: 'x', depends_on: { field: 'mode', values: ['a'] } }, { mode: 'a' })).toBe(true)
  })
  // apipkg op fields: depends_on names the operations, visible_for the exact
  // (resource, operation) pairs; "get" exists on two resources here.
  const taskId = {
    key: 'taskId', type: 'text',
    depends_on: { key: 'operation', values: ['get', 'update'] },
    visible_for: [{ resource: 'task', operation: 'get' }, { resource: 'task', operation: 'update' }],
  }
  it('honours visible_for exactly', () => {
    expect(fieldIsVisible(taskId, { resource: 'task', operation: 'get' })).toBe(true)
    expect(fieldIsVisible(taskId, { resource: 'task', operation: 'update' })).toBe(true)
    expect(fieldIsVisible(taskId, { resource: 'project', operation: 'get' })).toBe(false)
    expect(fieldIsVisible(taskId, { resource: 'task', operation: 'delete' })).toBe(false)
  })
  it('matches an empty resource for a package without resources', () => {
    const field = { key: 'q', visible_for: [{ resource: '', operation: 'search' }] }
    expect(fieldIsVisible(field, { operation: 'search' })).toBe(true)
    expect(fieldIsVisible(field, { operation: 'list' })).toBe(false)
  })
  it('uses defaults for unset visible_for keys', () => {
    const fields = [{ key: 'resource', default: 'task' }, { key: 'operation', default: 'get' }, taskId]
    expect(fieldIsVisible(taskId, {}, fields)).toBe(true)
  })
})

describe('collection fields', () => {
  it('shows an object as indented JSON and text as is', () => {
    expect(collectionText({ a: 1 })).toBe('{\n  "a": 1\n}')
    expect(collectionText('{"a":1}')).toBe('{"a":1}')
    expect(collectionText(undefined)).toBe('')
    expect(collectionText(null)).toBe('')
  })
  it('round-trips an object value unchanged', () => {
    const v = { timeout: 30, headers: { A: 'b' } }
    const r = commitCollection(collectionText(v), true)
    expect(r.error).toBe('')
    expect(r.value).toEqual(v)
  })
  it('keeps text values as text', () => {
    expect(commitCollection('{"a": 1}', false)).toEqual({ value: '{"a": 1}', error: '' })
  })
  it('does not save invalid text or non-objects', () => {
    expect(commitCollection('{"a":', true).error).toMatch(/Not valid JSON/)
    expect(commitCollection('{"a":', true).value).toBe(undefined)
    expect(commitCollection('[1,2]', false).error).toMatch(/JSON object/)
    expect(commitCollection('"x"', false).error).toMatch(/JSON object/)
  })
  it('saves empty and template values', () => {
    expect(commitCollection('  ', true)).toEqual({ value: {}, error: '' })
    expect(commitCollection('', false)).toEqual({ value: '', error: '' })
    expect(commitCollection('{{ $json.opts }}', true)).toEqual({ value: '{{ $json.opts }}', error: '' })
  })
})

describe('json code fields', () => {
  it('flags invalid JSON only', () => {
    expect(jsonTextError('{"a":1}')).toBe('')
    expect(jsonTextError('')).toBe('')
    expect(jsonTextError('{ "login": {{ $json.login }} }')).toBe('')
    expect(jsonTextError('{a:1}')).toMatch(/Not valid JSON/)
  })
})

describe('curl import', () => {
  it('resets request settings to defaults, keeps the connection, applies the import', () => {
    const current = { credential_id: 'c1', url: 'https://old', method: 'PUT', body: 'old', auth_type: 'bearer', extra: 'kept by caller' }
    const next = curlImportConfig(httpRequest.fields, current, { url: 'https://new', method: 'POST', headers: { A: 'b' } })
    expect(next).toEqual({
      credential_id: 'c1', url: 'https://new', method: 'POST', auth_type: 'none', credential_auth: '',
      body: '', follow_redirects: true, max_redirects: 10, headers: { A: 'b' },
    })
  })
})
