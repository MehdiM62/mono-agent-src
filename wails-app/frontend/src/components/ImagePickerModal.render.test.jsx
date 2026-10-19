// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import * as WailsApp from '../wailsjs/go/main/App'
import ImagePickerModal from './ImagePickerModal.jsx'

// The vault also holds files the extension saved (vault.save): a PDF or a
// Markdown file is not an image and must not be offered here.
vi.mock('../wailsjs/go/main/App', () => ({
  GetVaultImages: vi.fn(() => Promise.resolve([
    { id: 'img-001', filename: 'img-001.png', label: 'Logo', size_bytes: 10 },
    { id: 'img-002', filename: 'img-002.pdf', label: 'invoice.pdf', size_bytes: 20 },
    { id: 'img-003', filename: 'img-003.md', label: 'notes.md', size_bytes: 30 },
    { id: 'img-004', filename: 'img-004.JPG', label: 'Photo', size_bytes: 40 },
  ])),
  GetVaultImageData: vi.fn(() => Promise.resolve('data:image/png;base64,AAAA')),
}))

afterEach(cleanup)

describe('ImagePickerModal', () => {
  it('offers only images, never a saved PDF or text file', async () => {
    render(<ImagePickerModal onSelect={vi.fn()} onClose={vi.fn()} />)
    await screen.findByText('img-001')
    expect(screen.getByText('img-004')).toBeInTheDocument()
    expect(screen.queryByText('img-002')).toBeNull()
    expect(screen.queryByText('img-003')).toBeNull()
    await waitFor(() => expect(WailsApp.GetVaultImageData).toHaveBeenCalledTimes(2))
    expect(WailsApp.GetVaultImageData.mock.calls.map(c => c[0]).sort()).toEqual(['img-001', 'img-004'])
  })
})
