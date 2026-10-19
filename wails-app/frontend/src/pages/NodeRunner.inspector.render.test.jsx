// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/react'
import { Inspector } from './NodeRunner.jsx'

// The @ autocomplete on a media field offers vault images only, never a file
// the extension saved (vault.save) such as a PDF or a Markdown file.
vi.mock('../wailsjs/go/main/App', async (importOriginal) => ({
  ...await importOriginal(),
  GetVaultImages: vi.fn(() => Promise.resolve([
    { id: 'img-001', filename: 'img-001.png', label: 'Logo', url: '' },
    { id: 'img-002', filename: 'img-002.pdf', label: 'invoice.pdf', url: '' },
    { id: 'img-003', filename: 'img-003.md', label: 'notes.md', url: '' },
    { id: 'img-004', filename: 'img-004.JPG', label: 'Photo', url: '' },
  ])),
}))
vi.mock('../services/api.js', () => ({
  api: {}, notify: vi.fn(), reportRunFailure: vi.fn(), subscribeEvent: vi.fn(() => () => {}),
}))

afterEach(cleanup)

const node = {
  id: 'n1', subtype: 'test.post', label: 'Post', category: 'social', config: {},
  schema: { fields: [{ key: 'image_path', label: 'Image', type: 'text' }] },
}

describe('Inspector media field', () => {
  it('offers only images in the @ autocomplete', async () => {
    render(<Inspector node={node} onConfigChange={vi.fn()} onClose={vi.fn()} liveSchemas={{}} automationIds={new Set()} />)
    await Promise.resolve()
    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: '@' } })
    expect(await screen.findByText('@img-001')).toBeInTheDocument()
    expect(screen.getByText('@img-004')).toBeInTheDocument()
    expect(screen.queryByText('@img-002')).toBeNull()
    expect(screen.queryByText('@img-003')).toBeNull()
  })
})
