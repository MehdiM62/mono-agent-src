// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import * as WailsApp from '../wailsjs/go/main/App'
import ImageVault from './ImageVault.jsx'

// A file the extension saved (vault.save) shows as a file with its name, not
// as a broken <img>; images still get their thumbnail.
vi.mock('../wailsjs/go/main/App', () => ({
  GetVaultImages: vi.fn(() => Promise.resolve([
    { id: 'img-001', filename: 'img-001.png', label: 'Logo', source: 'upload', size_bytes: 10 },
    { id: 'img-002', filename: 'img-002.pdf', label: 'invoice.pdf', source: 'extension', size_bytes: 20 },
  ])),
  GetVaultStats: vi.fn(() => Promise.resolve({ count: 2, total_bytes: 30 })),
  GetVaultImageData: vi.fn(() => Promise.resolve('data:image/png;base64,AAAA')),
}))
vi.mock('../services/api.js', () => ({ onImagesChanged: () => () => {} }))
vi.mock('../components/ImageDetailModal', () => ({ default: () => null }))

afterEach(cleanup)

describe('ImageVault', () => {
  it('shows a saved PDF as a file with its name and only fetches thumbnails for images', async () => {
    const { container } = render(<ImageVault />)
    const file = await screen.findByRole('img', { name: 'file invoice.pdf' })
    expect(file).toHaveTextContent('PDF')
    expect(file.querySelector('img')).toBeNull()
    await waitFor(() => expect(container.querySelectorAll('img')).toHaveLength(1))
    expect(WailsApp.GetVaultImageData).toHaveBeenCalledTimes(1)
    expect(WailsApp.GetVaultImageData).toHaveBeenCalledWith('img-001')
  })
})
