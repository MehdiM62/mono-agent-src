// @vitest-environment jsdom
import React from 'react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, cleanup, waitFor } from '@testing-library/react'
import '../../i18n.js'

vi.mock('../../services/api.js', () => ({ api: { getActiveProfile: () => Promise.resolve({ name: 'Work', root_dir: '/p/work' }) }, notify: vi.fn() }))
import { FolderPicker } from './FolderPicker.jsx'

afterEach(cleanup)
const status = { enabled: true, runtimes: [{ id: 'claude', ready: true, installed: true }, { id: 'gemini', ready: false, installed: false }] }

describe('FolderPicker', () => {
  it('offers the profile folder by default, a native pick and recent folders, and reports the choice', async () => {
    const onWorkspaceChange = vi.fn()
    const onPickFolder = vi.fn()
    render(<FolderPicker status={status} workspace={{ kind: 'root' }} onWorkspaceChange={onWorkspaceChange} onPickFolder={onPickFolder}
      recent={[{ path: '/src/my-repo', conversations: 2 }]} runtimeId="claude" />)
    await waitFor(() => expect(screen.getByRole('radio', { name: /Profile folder \(default\): Work/ })).toBeChecked())
    fireEvent.click(screen.getByRole('radio', { name: /my-repo/ }))
    expect(onWorkspaceChange).toHaveBeenCalledWith({ kind: 'folder', path: '/src/my-repo' })
    fireEvent.click(screen.getByRole('radio', { name: /Choose another folder/ }))
    expect(onPickFolder).toHaveBeenCalled()
    expect(screen.getByText(/will run commands and change files in this folder without asking/)).toBeInTheDocument()
    expect(screen.getByTestId('coder-runtimes')).toHaveTextContent('ready')
  })

  it('uses a profile its parent already loaded', () => {
    render(<FolderPicker status={status} workspace={{ kind: 'root' }} onWorkspaceChange={() => {}} profile={{ name: 'Home', root_dir: '/p/home' }} />)
    expect(screen.getByRole('radio', { name: /Profile folder \(default\): Home/ })).toBeInTheDocument()
  })
})
