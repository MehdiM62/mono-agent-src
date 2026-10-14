// @vitest-environment jsdom
import { describe, it, expect } from 'vitest'
import { formFromJSON, jsonFromForm, parseTriggerInput, rememberTriggerInput, rememberedTriggerInput } from './triggerInput.js'

// Which fields a workflow reads is the CLI's answer (`workflow inputs
// --json`, covered by internal/workflow/trigger_inputs_test.go). What is
// tested here is what the dialog itself owns: validating the typed payload
// and remembering it between runs.

describe('parseTriggerInput', () => {
  it('accepts an object and sends it compacted', () => {
    expect(parseTriggerInput('{\n "prompts": ["a wizard"]\n}')).toEqual({ ok: true, value: '{"prompts":["a wizard"]}' })
  })

  it('treats empty and {} as no trigger data', () => {
    expect(parseTriggerInput('')).toEqual({ ok: true, value: '' })
    expect(parseTriggerInput('  {}  ')).toEqual({ ok: true, value: '' })
  })

  it('rejects malformed JSON and non-objects with a usable message', () => {
    expect(parseTriggerInput('{"prompts": [}').ok).toBe(false)
    const arr = parseTriggerInput('["a"]')
    expect(arr.ok).toBe(false)
    expect(arr.error).toMatch(/must be a JSON object/)
    expect(parseTriggerInput('"hello"').ok).toBe(false)
    expect(parseTriggerInput('null').ok).toBe(false)
  })
})

describe('remembering trigger input', () => {
  const skeleton = { prompts: ['generate an image that: a wizard'] }

  it('opens with what was run last time for this workflow', () => {
    rememberTriggerInput('wf-1', '{"prompts":["a wizard"]}')
    expect(rememberedTriggerInput('wf-1', skeleton)).toBe('{\n  "prompts": [\n    "a wizard"\n  ]\n}')
  })

  it('falls back to the skeleton the CLI supplied', () => {
    expect(rememberedTriggerInput('wf-unknown', skeleton))
      .toBe('{\n  "prompts": [\n    "generate an image that: a wizard"\n  ]\n}')
    expect(rememberedTriggerInput('wf-unknown', undefined)).toBe('{}')
  })

  it('survives unreadable or blocked storage', () => {
    rememberTriggerInput('wf-2', '{"a":1}')
    localStorage.setItem('monoagent:wf-trigger-input:wf-2', 'not json')
    expect(rememberedTriggerInput('wf-2', skeleton)).toContain('generate an image that')

    const getItem = Storage.prototype.getItem
    Storage.prototype.getItem = () => { throw new Error('blocked') }
    try {
      expect(rememberedTriggerInput('wf-1', skeleton)).toContain('generate an image that')
    } finally {
      Storage.prototype.getItem = getItem
    }
  })

  it('clears the remembered value when a run supplies none', () => {
    rememberTriggerInput('wf-3', '{"a":1}')
    rememberTriggerInput('wf-3', '')
    expect(rememberedTriggerInput('wf-3', skeleton)).toContain('generate an image that')
  })
})

describe('trigger input form', () => {
  const fields = [{ name: 'prompt', structured: false }, { name: 'prompts', structured: true }]

  it('splits JSON into per-field values and builds it back', () => {
    const form = formFromJSON('{"prompt":"a red bicycle","prompts":["a","b"],"other":1}', fields)
    expect(form.values.prompt).toBe('a red bicycle')
    expect(form.asJSON).toEqual({ prompt: false, prompts: true })
    expect(JSON.parse(form.values.prompts)).toEqual(['a', 'b'])
    const back = jsonFromForm(form, fields)
    expect(back.ok).toBe(true)
    expect(JSON.parse(back.text)).toEqual({ prompt: 'a red bicycle', prompts: ['a', 'b'], other: 1 })
  })

  it('leaves empty fields out and names a field with bad JSON', () => {
    const form = formFromJSON('{}', fields)
    expect(JSON.parse(jsonFromForm(form, fields).text)).toEqual({})
    form.values.prompts = '["a",'
    const bad = jsonFromForm(form, fields)
    expect(bad.ok).toBe(false)
    expect(bad.error).toMatch(/^prompts: not valid JSON/)
  })

  it('has no form for text that is not a JSON object', () => {
    expect(formFromJSON('{oops', fields)).toBeNull()
    expect(formFromJSON('[1]', fields)).toBeNull()
  })
})
