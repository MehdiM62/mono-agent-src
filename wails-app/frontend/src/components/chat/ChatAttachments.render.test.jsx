// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react'

vi.mock('../../services/api.js', () => ({ api: { readWorkspaceImage: vi.fn(), openURL: vi.fn(), coderStopBackground: vi.fn() } }))
import { api } from '../../services/api.js'

import { ChatComposer } from './ChatComposer.jsx'
import { MessageAttachments, WorkspaceImageContext } from './ChatAttachments.jsx'
import { ChatTimeline } from './ChatTimeline.jsx'
import { chatReducer, initialChatState } from './chatReducer.js'

afterEach(() => { cleanup(); vi.clearAllMocks() })

const PNG = 'data:image/png;base64,iVBORw0KGgo='

describe('ChatComposer attachments', () => {
  const img = { id: 'a1', name: 'shot.png', mime: 'image/png', size: 2048, dataUrl: PNG }
  const doc = { id: 'a2', name: 'notes.txt', mime: 'text/plain', size: 5, dataUrl: 'data:text/plain;base64,aGVsbG8=' }

  function composer(props = {}) {
    const p = { value: 'look', onChange: vi.fn(), onSend: vi.fn(), onStop: vi.fn(), streaming: false, onAttach: vi.fn(), onRemoveAttachment: vi.fn(), ...props }
    render(<ChatComposer {...p} />)
    return p
  }

  it('shows an image chip and a file chip, each removable', () => {
    const p = composer({ attachments: [img, doc] })
    expect(screen.getAllByTestId('attachment-chip')).toHaveLength(2)
    expect(screen.getByRole('img', { name: 'shot.png' })).toHaveAttribute('src', PNG)
    expect(screen.getByText('notes.txt')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Remove notes.txt' }))
    expect(p.onRemoveAttachment).toHaveBeenCalledWith('a2')
    // A chip's image opens large.
    fireEvent.click(screen.getByRole('button', { name: 'Open shot.png' }))
    expect(screen.getByTestId('image-lightbox')).toBeInTheDocument()
  })

  it('attaches pasted files, but a paste with text stays text', () => {
    const p = composer()
    const file = new File(['x'], 'image.png', { type: 'image/png' })
    const box = screen.getByRole('textbox')
    fireEvent.paste(box, { clipboardData: { files: [file], getData: () => '' } })
    expect(p.onAttach).toHaveBeenCalledWith([file])
    fireEvent.paste(box, { clipboardData: { files: [file], getData: () => 'A1\tB1' } })
    expect(p.onAttach).toHaveBeenCalledTimes(1)
  })

  it('attaches dropped and picked files', () => {
    const p = composer()
    const file = new File(['x'], 'a.log', { type: 'text/plain' })
    fireEvent.drop(screen.getByTestId('chat-composer'), { dataTransfer: { files: [file], types: ['Files'] } })
    expect(p.onAttach).toHaveBeenLastCalledWith([file])
    fireEvent.change(screen.getByTestId('attach-input'), { target: { files: [file] } })
    expect(p.onAttach).toHaveBeenCalledTimes(2)
  })

  it('has no attach control without onAttach (the assistant chat)', () => {
    composer({ onAttach: undefined })
    expect(screen.queryByRole('button', { name: 'Attach files' })).toBeNull()
    expect(screen.queryByTestId('attach-input')).toBeNull()
  })
})

describe('MessageAttachments', () => {
  it('shows a sent image from memory and a stored one from the folder, files as chips', async () => {
    api.readWorkspaceImage.mockResolvedValue(PNG)
    render(<MessageAttachments conversationId="c1" attachments={[
      { name: 'now.png', mime: 'image/png', size: 10, dataUrl: PNG },
      { name: 'old.png', mime: 'image/png', size: 10, path: '/w/.monoagent/attachments/t1/old.png' },
      { name: 'data.csv', mime: 'text/csv', size: 2048, path: '/w/.monoagent/attachments/t1/data.csv' },
    ]} />)
    expect(screen.getByTestId('message-image')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('img', { name: 'old.png' })).toBeInTheDocument())
    expect(api.readWorkspaceImage).toHaveBeenCalledWith('c1', '/w/.monoagent/attachments/t1/old.png')
    expect(api.readWorkspaceImage).toHaveBeenCalledTimes(1)
    expect(screen.getByText('data.csv')).toBeInTheDocument()
    expect(screen.getByText('2 KB')).toBeInTheDocument()
  })
})

let seq = 0
function ev(type, payload) {
  seq += 1
  return { type: 'event', event: { conversationId: 'c1', turnId: 't1', seq, at: '2026-10-15T10:00:00.000Z', type, payload } }
}

function screenshotTurn(result, ok = true) {
  return [
    ev('turn.started', { backend: 'agent', text: 'shoot', attachments: [{ name: 'a.png', path: '/w/a.png', mime: 'image/png', size: 3 }] }),
    ev('tool.started', { callId: 'b1', name: 'Bash', native: true, kind: 'shell', arguments: { command: 'monoagentcli browser screenshot --json' } }),
    ev('tool.completed', { callId: 'b1', ok, result }),
  ].reduce(chatReducer, initialChatState())
}

describe('tool cards with images', () => {
  const result = JSON.stringify({ path: '/w/app/.monoagent/screenshots/one.png', width: 1280, height: 800 })

  it('keeps turn.started attachments in the turn state', () => {
    expect(screenshotTurn(result).attachments).toEqual([{ name: 'a.png', path: '/w/a.png', mime: 'image/png', size: 3 }])
    expect('attachments' in [ev('turn.started', { backend: 'agent', text: 'hi' })].reduce(chatReducer, initialChatState())).toBe(false)
  })

  it('shows the screenshot a coder tool call names, and opens it large', async () => {
    api.readWorkspaceImage.mockResolvedValue(PNG)
    render(<WorkspaceImageContext.Provider value="c1"><ChatTimeline state={screenshotTurn(result)} turnId="t1" isLive={false} /></WorkspaceImageContext.Provider>)
    const img = await screen.findByRole('img', { name: 'one.png' })
    expect(img).toHaveAttribute('src', PNG)
    expect(api.readWorkspaceImage).toHaveBeenCalledWith('c1', '/w/app/.monoagent/screenshots/one.png')
    fireEvent.click(screen.getByRole('button', { name: 'Open one.png' }))
    expect(screen.getByTestId('image-lightbox')).toBeInTheDocument()
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByTestId('image-lightbox')).toBeNull()
  })

  it('shows nothing outside a coder chat, for a failed call, or when the image cannot be read', async () => {
    const { unmount } = render(<ChatTimeline state={screenshotTurn(result)} turnId="t1" isLive={false} />)
    expect(screen.queryByTestId('tool-result-images')).toBeNull()
    unmount()
    const failed = render(<WorkspaceImageContext.Provider value="c1"><ChatTimeline state={screenshotTurn(result, false)} turnId="t1" isLive={false} /></WorkspaceImageContext.Provider>)
    expect(screen.queryByTestId('tool-result-images')).toBeNull()
    failed.unmount()
    expect(api.readWorkspaceImage).not.toHaveBeenCalled()

    api.readWorkspaceImage.mockRejectedValue(new Error('outside this chat'))
    const other = JSON.stringify({ path: '/etc/elsewhere.png' })
    render(<WorkspaceImageContext.Provider value="c2"><ChatTimeline state={screenshotTurn(other)} turnId="t1" isLive={false} /></WorkspaceImageContext.Provider>)
    await waitFor(() => expect(api.readWorkspaceImage).toHaveBeenCalledWith('c2', '/etc/elsewhere.png'))
    await waitFor(() => expect(screen.queryByTestId('workspace-image-loading')).toBeNull())
    expect(screen.queryByRole('img')).toBeNull()
  })
})
