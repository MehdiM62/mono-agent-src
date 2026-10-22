// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/react'
import { ChatComposer } from './ChatComposer.jsx'

afterEach(cleanup)

// ── ChatComposer: IME-safe Enter ─────────────────────────────────────────────

describe('ChatComposer', () => {
  it('sends on a plain Enter', () => {
    const onSend = vi.fn()
    render(<ChatComposer value="hi" onChange={() => {}} onSend={onSend} onStop={() => {}} streaming={false} disabled={false} />)
    fireEvent.keyDown(screen.getByPlaceholderText('Type a message...'), { key: 'Enter' })
    expect(onSend).toHaveBeenCalledTimes(1)
  })

  it('never sends while an IME composition is in progress', () => {
    const onSend = vi.fn()
    render(<ChatComposer value="日本語" onChange={() => {}} onSend={onSend} onStop={() => {}} streaming={false} disabled={false} />)
    const textarea = screen.getByPlaceholderText('Type a message...')
    fireEvent.compositionStart(textarea)
    fireEvent.keyDown(textarea, { key: 'Enter', isComposing: true })
    expect(onSend).not.toHaveBeenCalled()
  })

  it('catches the stray keyCode-229 Enter some browsers fire right as composition ends', () => {
    const onSend = vi.fn()
    render(<ChatComposer value="hi" onChange={() => {}} onSend={onSend} onStop={() => {}} streaming={false} disabled={false} />)
    const textarea = screen.getByPlaceholderText('Type a message...')
    fireEvent.compositionStart(textarea)
    fireEvent.compositionEnd(textarea)
    fireEvent.keyDown(textarea, { key: 'Enter', keyCode: 229 })
    expect(onSend).not.toHaveBeenCalled()
  })

  it('sends normally once composition has actually ended', () => {
    const onSend = vi.fn()
    render(<ChatComposer value="hi" onChange={() => {}} onSend={onSend} onStop={() => {}} streaming={false} disabled={false} />)
    const textarea = screen.getByPlaceholderText('Type a message...')
    fireEvent.compositionStart(textarea)
    fireEvent.compositionEnd(textarea)
    fireEvent.keyDown(textarea, { key: 'Enter' })
    expect(onSend).toHaveBeenCalledTimes(1)
  })

  it('never sends on Shift+Enter — inserts a newline instead', () => {
    const onSend = vi.fn()
    render(<ChatComposer value="hi" onChange={() => {}} onSend={onSend} onStop={() => {}} streaming={false} disabled={false} />)
    fireEvent.keyDown(screen.getByPlaceholderText('Type a message...'), { key: 'Enter', shiftKey: true })
    expect(onSend).not.toHaveBeenCalled()
  })

  it('shows Stop instead of Send while streaming, and it stays clickable', () => {
    const onStop = vi.fn()
    render(<ChatComposer value="" onChange={() => {}} onSend={() => {}} onStop={onStop} streaming={true} disabled={false} />)
    fireEvent.click(screen.getByRole('button', { name: 'Stop generating' }))
    expect(onStop).toHaveBeenCalledTimes(1)
  })

  it('disables Send with an explicit reason when no backend is selected', () => {
    render(<ChatComposer value="hi" onChange={() => {}} onSend={() => {}} onStop={() => {}} streaming={false} disabled={true} disabledReason="Select an agent runtime above to start chatting" />)
    expect(screen.getByText('Select an agent runtime above to start chatting')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Send message' })).toBeDisabled()
  })
})
