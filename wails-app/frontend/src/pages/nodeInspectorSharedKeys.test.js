import { describe, it, expect } from 'vitest'
import { fieldIsVisible, fieldValueFor, configDefaults, configAfterChange } from './nodeInspectorRules.js'
// Real schemas, from `monoagentcli --json node schema <type>`.
import openai from './__fixtures__/schema-app.openai_api.json'
import anthropic from './__fixtures__/schema-app.anthropic_api.json'
import memory from './__fixtures__/schema-agent.memory.json'

const shown = (schema, cfg) => schema.fields.filter(f => fieldIsVisible(f, cfg, schema.fields))
const labels = (schema, cfg) => shown(schema, cfg).map(f => f.label)
const opSelect = (schema, resource) => schema.fields.find(f => f.key === 'operation' && f.depends_on?.values?.includes(resource))
const resources = schema => schema.fields.find(f => f.key === 'resource').options

describe('API package nodes: an operation select per resource', () => {
  it('a new node starts on the default resource\'s operation, not the last one listed', () => {
    const d = configDefaults(openai.fields)
    expect(d.resource).toBe('text')
    expect(d.operation).toBe('classify')
    expect(labels(openai, d)).toEqual(expect.arrayContaining(['Text Operation', 'Text', 'Simplify', 'Options', 'Legacy moderation models']))
    const a = configDefaults(anthropic.fields)
    expect([a.resource, a.operation]).toEqual(['file', 'upload'])
    expect(labels(anthropic, a)).toEqual(expect.arrayContaining(['File Operation', 'Input', 'File URL']))
  })

  for (const [name, schema] of [['openai', openai], ['anthropic', anthropic]]) {
    it(`${name}: switching resource selects that resource's default operation`, () => {
      let cfg = configDefaults(schema.fields)
      for (const r of resources(schema)) {
        cfg = configAfterChange(schema.fields, cfg, 'resource', r)
        const sel = opSelect(schema, r)
        expect(cfg.operation, r).toBe(sel.default)
        // Only this resource's operation select shows.
        expect(shown(schema, cfg).filter(f => f.key === 'operation')).toEqual([sel])
        expect(fieldValueFor(sel, cfg, schema.fields)).toBe(sel.default)
      }
    })

    it(`${name}: every resource and operation shows exactly its own fields`, () => {
      let cfg = configDefaults(schema.fields)
      for (const r of resources(schema)) {
        cfg = configAfterChange(schema.fields, cfg, 'resource', r)
        for (const op of opSelect(schema, r).options) {
          cfg = configAfterChange(schema.fields, cfg, 'operation', op)
          const want = schema.fields.filter(f => !f.visible_for && !f.depends_on ||
            f === opSelect(schema, r) ||
            (f.visible_for || []).some(c => c.resource === r && c.operation === op))
          expect(shown(schema, cfg), `${r}/${op}`).toEqual(want)
        }
      }
    })
  }

  it('resets a shared key whose field changed with the operation', () => {
    let cfg = configDefaults(openai.fields)
    cfg = configAfterChange(openai.fields, cfg, 'resource', 'audio')
    expect(cfg.model).toBe('tts-1') // the audio model select
    cfg = configAfterChange(openai.fields, cfg, 'resource', 'image')
    expect([cfg.operation, cfg.model]).toEqual(['generate', '']) // image generate's model text
    cfg = configAfterChange(openai.fields, cfg, 'options', '{"n": 2}')
    cfg = configAfterChange(openai.fields, cfg, 'operation', 'edit')
    expect(cfg.options).toEqual({}) // edit's own options, not generate's
    expect(cfg.size).toBe('1024x1024')
    cfg = configAfterChange(openai.fields, cfg, 'resource', 'video')
    expect(cfg.size).toBe('1280x720')
  })

  it('keeps what the user typed when no shared field moves', () => {
    let cfg = configDefaults(openai.fields)
    cfg = configAfterChange(openai.fields, cfg, 'resource', 'image')
    cfg = configAfterChange(openai.fields, cfg, 'prompt', 'a cat')
    cfg = configAfterChange(openai.fields, cfg, 'options', '{"n": 2}')
    cfg = configAfterChange(openai.fields, cfg, 'legacyImageModel', true)
    expect(cfg).toMatchObject({ resource: 'image', operation: 'generate', prompt: 'a cat', options: '{"n": 2}', legacyImageModel: true })
  })

  it('reads a stale operation from another resource as this resource\'s default', () => {
    const cfg = { resource: 'image', operation: 'classify' }
    const sel = opSelect(openai, 'image')
    expect(fieldValueFor(sel, cfg, openai.fields)).toBe('generate')
    expect(labels(openai, cfg)).toContain('Prompt')
    // A template is left alone.
    expect(fieldValueFor(sel, { resource: 'image', operation: '{{ $json.op }}' }, openai.fields)).toBe('{{ $json.op }}')
  })
})

describe('a node with built-in operations and API package resources', () => {
  // service.* nodes: the built-in Operation select has no rule, so it shows
  // next to the resource's own operation select.
  const fields = [
    { key: 'operation', type: 'select', default: 'send', options: ['send', 'list'] },
    { key: 'resource', type: 'select', default: '', options: ['', 'message'] },
    { key: 'operation', type: 'select', default: 'post', options: ['post', 'getPermalink'], depends_on: { key: 'resource', values: ['message'] } },
    { key: 'ts', type: 'text', visible_for: [{ resource: 'message', operation: 'getPermalink' }] },
  ]
  it('starts on the built-in operation', () => {
    expect(configDefaults(fields)).toEqual({ operation: 'send', resource: '', ts: '' })
  })
  it('reads a resource operation through the select that offers it', () => {
    const cfg = configAfterChange(fields, { operation: 'send', resource: 'message' }, 'operation', 'getPermalink')
    expect(cfg.operation).toBe('getPermalink')
    expect(fieldIsVisible(fields[3], cfg, fields)).toBe(true)
    expect(fieldValueFor(fields[2], cfg, fields)).toBe('getPermalink')
  })
})

describe('selects with a key of their own', () => {
  const fields = [{ key: 'method', type: 'select', default: 'GET', options: ['GET', 'POST'] }, { key: 'url', type: 'text' }]
  it('keep an imported value the options do not list', () => {
    expect(fieldValueFor(fields[0], { method: 'HEAD' }, fields)).toBe('HEAD')
    expect(configAfterChange(fields, { method: 'HEAD' }, 'url', 'https://x')).toEqual({ method: 'HEAD', url: 'https://x' })
    expect(configDefaults(fields)).toEqual({ method: 'GET', url: '' })
  })
})

describe('a field under a hidden field stays hidden', () => {
  const count = memory.fields.find(f => f.key === 'count')
  it('agent.memory shows "Messages to Delete" only for delete with last_n', () => {
    expect(fieldIsVisible(count, { operation: 'get' }, memory.fields)).toBe(false)
    expect(fieldIsVisible(count, { operation: 'insert', delete_mode: 'last_n' }, memory.fields)).toBe(false)
    expect(fieldIsVisible(count, { operation: 'delete' }, memory.fields)).toBe(true)
    expect(fieldIsVisible(count, { operation: 'delete', delete_mode: 'all' }, memory.fields)).toBe(false)
    expect(labels(memory, configDefaults(memory.fields))).toEqual(['Operation', 'Session Key', 'Format', 'Group Messages'])
  })
  it('survives rules that loop', () => {
    const loop = [
      { key: 'a', type: 'select', default: 'x', options: ['x'], depends_on: { key: 'b', values: ['y'] } },
      { key: 'b', type: 'select', default: 'y', options: ['y'], depends_on: { key: 'a', values: ['x'] } },
    ]
    expect(fieldIsVisible(loop[0], {}, loop)).toBe(true)
    expect(configDefaults(loop)).toEqual({ a: 'x', b: 'y' })
  })
})
