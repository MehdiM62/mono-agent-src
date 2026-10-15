// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, within, cleanup } from '@testing-library/react'
import BrowserPermissionsSection from './BrowserPermissionsSection.jsx'
import { api } from '../../services/api.js'
// The `monoagentcli browser permissions --json` document (plan C8).
import fixture from './__fixtures__/browser-permissions.json'

vi.mock('../../services/api.js', () => ({
  api: { browserPermissions: vi.fn(), setBrowserPermission: vi.fn(), setBrowserSiteRule: vi.fn() },
}))

const doc = (patch = {}) => ({ ...structuredClone(fixture), ...patch })

beforeEach(() => {
  api.browserPermissions.mockResolvedValue(doc())
  api.setBrowserPermission.mockResolvedValue(doc())
  api.setBrowserSiteRule.mockResolvedValue(doc())
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

const kindRow = (kind) => document.querySelector(`[data-kind="${kind}"]`)

describe('BrowserPermissionsSection', () => {
  it('shows a level picker per agent kind with the catalog labels and descriptions, and marks defaults', async () => {
    render(<BrowserPermissionsSection />)
    expect(await screen.findByText('Coder chats')).toBeInTheDocument()
    expect(['coder', 'org', 'chat', 'other'].map(k => within(kindRow(k)).getByRole('combobox').value)).toEqual(['dev', 'full', 'full', 'ask'])
    const options = within(kindRow('coder')).getAllByRole('option').map(o => o.textContent)
    expect(options).toEqual([
      '⚠ Full, no approvals — Any site; form submits go through without asking you.',
      'Full — submits ask — Any site; a form submit on a real site waits for your approval.',
      'Dev pages + allowlist — Local and dev pages plus the sites you allow; submits on real sites ask.',
      'Ask per new site — Like Dev pages + allowlist, but a new site asks you instead of being refused.',
      'Off — No browser use at all.',
    ])
    expect(within(kindRow('coder')).getByText('Local and dev pages plus the sites you allow; submits on real sites ask.')).toBeInTheDocument()
    expect(within(kindRow('coder')).getByText('default')).toBeInTheDocument()
    expect(within(kindRow('other')).queryByText('default')).toBeNull()
    expect(screen.queryByTestId('permission-risk')).toBeNull()
  })

  it('sets a level through the CLI, reads it back, and warns on the riskiest level', async () => {
    render(<BrowserPermissionsSection />)
    await screen.findByText('Coder chats')
    const risky = doc()
    risky.kinds[0] = { ...risky.kinds[0], level: 'full-no-approvals', isDefault: false }
    api.browserPermissions.mockResolvedValue(risky)
    fireEvent.change(within(kindRow('coder')).getByRole('combobox'), { target: { value: 'full-no-approvals' } })
    expect(api.setBrowserPermission).toHaveBeenCalledWith('coder', 'full-no-approvals')
    const warning = await screen.findByTestId('permission-risk')
    expect(kindRow('coder')).toContainElement(warning)
    expect(warning).toHaveTextContent('riskiest level')
  })

  it('lists site rules, adds one and removes one', async () => {
    render(<BrowserPermissionsSection />)
    await screen.findByText('https://github.com')
    const github = document.querySelector('[data-origin="https://github.com"]')
    expect(within(github).getByText('Allow')).toBeInTheDocument()
    expect(within(document.querySelector('[data-origin="https://bank.example.com"]')).getByText('Block')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Site'), { target: { value: ' https://news.example.com ' } })
    fireEvent.change(screen.getByLabelText('Rule for the site'), { target: { value: 'block' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add rule' }))
    await waitFor(() => expect(api.setBrowserSiteRule).toHaveBeenCalledWith('https://news.example.com', 'block'))
    await waitFor(() => expect(screen.getByLabelText('Site')).toHaveValue(''))

    fireEvent.click(within(github).getByRole('button', { name: 'Remove the rule for https://github.com' }))
    await waitFor(() => expect(api.setBrowserSiteRule).toHaveBeenLastCalledWith('https://github.com', 'clear'))
    expect(api.browserPermissions).toHaveBeenCalledTimes(3)
  })

  it('shows the CLI refusal and keeps the typed site', async () => {
    api.setBrowserSiteRule.mockRejectedValue(new Error('"ftp://x" is not an origin: use scheme://host[:port]'))
    render(<BrowserPermissionsSection />)
    await screen.findByText('Coder chats')
    fireEvent.change(screen.getByLabelText('Site'), { target: { value: 'ftp://x' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add rule' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('is not an origin')
    expect(screen.getByLabelText('Site')).toHaveValue('ftp://x')
  })

  it('says so when the settings cannot be read', async () => {
    api.browserPermissions.mockRejectedValue(new Error('the installed monoagentcli has no browser permissions command'))
    render(<BrowserPermissionsSection />)
    expect(await screen.findByRole('alert')).toHaveTextContent('no browser permissions command')
  })
})
