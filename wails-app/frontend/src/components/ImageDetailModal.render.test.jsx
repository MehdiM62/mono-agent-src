// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import * as WailsApp from '../wailsjs/go/main/App'
import ImageDetailModal from './ImageDetailModal.jsx'

// A file the extension saved (vault.save) opens as a file: its icon, name and
// kind, with no image fetch and no image editor.
vi.mock('../wailsjs/go/main/App', () => ({
  GetVaultImageData: vi.fn(() => Promise.resolve('data:image/png;base64,AAAA')),
}))
vi.mock('./ImageEditorFullscreen', () => ({ default: () => null }))

afterEach(() => { cleanup(); vi.clearAllMocks() })

const pdf = { id: 'img-002', filename: 'img-002.pdf', label: 'invoice.pdf', source: 'extension', size_bytes: 20 }
const png = { id: 'img-001', filename: 'img-001.png', label: 'Logo', source: 'upload', size_bytes: 10 }

describe('ImageDetailModal', () => {
  it('shows a saved PDF as a file and never fetches it as an image', async () => {
    const { container } = render(<ImageDetailModal image={pdf} onClose={vi.fn()} onDelete={vi.fn()} />)
    const file = screen.getByRole('img', { name: 'file invoice.pdf' })
    expect(file).toHaveTextContent('invoice.pdf')
    expect(file).toHaveTextContent('PDF')
    expect(container.querySelector('img')).toBeNull()
    expect(screen.queryByText('Loading…')).toBeNull()
    expect(screen.queryByRole('button', { name: /Adjust Image/ })).toBeNull()
    expect(screen.getByRole('button', { name: /Save File/ })).toBeInTheDocument()
    await Promise.resolve()
    expect(WailsApp.GetVaultImageData).not.toHaveBeenCalled()
  })

  it('still previews an image and offers the editor', async () => {
    const { container } = render(<ImageDetailModal image={png} onClose={vi.fn()} onDelete={vi.fn()} />)
    await waitFor(() => expect(container.querySelector('img')).toHaveAttribute('src', 'data:image/png;base64,AAAA'))
    expect(WailsApp.GetVaultImageData).toHaveBeenCalledWith('img-001')
    expect(screen.getByRole('button', { name: /Adjust Image/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Save Image/ })).toBeInTheDocument()
  })
})
