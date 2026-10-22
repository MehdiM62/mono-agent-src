import { describe, it, expect } from 'vitest'
import { isRaise, accessOfConversation, browserKindFor, isConfinedTask, defaultAccessFor, chosenAccess } from './chatAccess.js'

describe('chat access rules', () => {
  it('a move up the order is a raise, a move down is not, an unknown level always asks', () => {
    expect(isRaise('scoped', 'coding')).toBe(true)
    expect(isRaise('coding', 'full')).toBe(true)
    expect(isRaise('full', 'coding')).toBe(false)
    expect(isRaise('workspace', 'workspace')).toBe(false)
    expect(isRaise('', 'scoped')).toBe(true)
  })

  it('reads a record without a level from its mode', () => {
    expect(accessOfConversation({ access: 'coding', mode: 'coder' })).toBe('coding')
    expect(accessOfConversation({ mode: 'coder' })).toBe('full')
    expect(accessOfConversation({ mode: 'assistant' })).toBe('scoped')
    expect(accessOfConversation({})).toBe('')
  })

  it('maps scoped to the chat browser kind and every coding level to coder', () => {
    expect(browserKindFor('scoped')).toBe('chat')
    for (const a of ['coding', 'workspace', 'full']) expect(browserKindFor(a)).toBe('coder')
  })

  it('only an application-apply task is confined', () => {
    expect(isConfinedTask({ kind: 'application-apply' })).toBe(true)
    expect(isConfinedTask({ kind: 'generic' })).toBe(false)
    expect(isConfinedTask(null)).toBe(false)
  })

  it('defaults to the coder default only when coder is on and a runtime is ready', () => {
    expect(defaultAccessFor({ enabled: true, defaultAccess: 'workspace' }, true)).toBe('workspace')
    expect(defaultAccessFor({ enabled: true, defaultAccess: 'workspace' }, false)).toBe('scoped')
    expect(defaultAccessFor({ enabled: false, defaultAccess: 'workspace' }, true)).toBe('scoped')
    expect(defaultAccessFor({ enabled: true }, true)).toBe('scoped')
  })

  it('holds a picked coding level to scoped while coder cannot run', () => {
    expect(chosenAccess('full', { enabled: false }, false)).toBe('scoped')
    expect(chosenAccess('full', { enabled: true }, true)).toBe('full')
    expect(chosenAccess('', { enabled: true, defaultAccess: 'coding' }, true)).toBe('coding')
  })
})
