// @vitest-environment jsdom
import { vi } from 'vitest'
import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { newTurnId } from './turnIds.js'
import { composeLiveAnnouncement } from './liveAnnouncement.js'
import { MessageBubble } from './messageBubble.jsx'

vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: k => k }) }))

// The three pieces the bubbles share, imported from chat/ (not from the
// assistant panel they came out of).
describe('chat shared exports', () => {
  it('newTurnId makes distinct v4 ids', () => {
    const a = newTurnId()
    expect(a).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    expect(newTurnId()).not.toBe(a)
  })

  it('composeLiveAnnouncement names the outcome, failed calls and unannounced notices', () => {
    const state = {
      terminal: { status: 'completed' }, calls: { a: { status: 'completed', ok: false } },
      notices: [{ message: 'old' }, { message: 'new notice' }],
    }
    const text = composeLiveAnnouncement(state, 1)
    expect(text).toContain('Response')
    expect(text).toContain('1 tool call failed.')
    expect(text).toContain('new notice')
    expect(text).not.toContain('old')
  })

  it('MessageBubble shows a user message as plain text', () => {
    render(<MessageBubble role="user" content="1. not a list" />)
    expect(screen.getByText('1. not a list')).toBeTruthy()
  })
})
