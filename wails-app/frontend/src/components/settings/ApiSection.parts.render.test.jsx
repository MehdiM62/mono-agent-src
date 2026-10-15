// @vitest-environment jsdom
// The API section is split by scope: part="machine" is the server's settings without the API keys, which are the
// active profile's and are shown by ApiKeysSection.
import React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'
import i18n from '../../i18n.js'
import { mainListener, statusOf, modelsDoc, keyList } from './api/__fixtures__/apiFixtures.js'
import { configDoc } from './api/__fixtures__/configFixtures.js'

const App = {}
beforeEach(async () => {
  vi.clearAllMocks()
  await i18n.changeLanguage('en')
  for (const k of ['APIStatus', 'APIKeyList', 'APIModels', 'APIConfigShow']) App[k] = vi.fn()
  App.APIStatus.mockResolvedValue(statusOf([mainListener()]))
  App.APIKeyList.mockResolvedValue(keyList())
  App.APIModels.mockResolvedValue(modelsDoc())
  App.APIConfigShow.mockResolvedValue(configDoc())
  window.go = { main: { App } }
})
afterEach(() => { cleanup(); delete window.go })

describe('API section parts', () => {
  it('the machine part never reads or shows the API keys', async () => {
    const { default: ApiSection } = await import('./ApiSection.jsx')
    render(<ApiSection part="machine" />)
    await waitFor(() => expect(screen.getByTestId('api-fold-state')).not.toHaveTextContent('Loading…'))
    expect(screen.queryByTestId('api-fold-keys')).not.toBeInTheDocument()
    fireEvent.click(screen.getByTestId('api-fold-toggle'))
    await waitFor(() => expect(App.APIConfigShow).toHaveBeenCalled())
    expect(App.APIKeyList).not.toHaveBeenCalled()
    expect(screen.queryByText('Create key')).not.toBeInTheDocument()
  })

  it('the keys part reads and shows the keys on its own', async () => {
    const { default: ApiKeysSection } = await import('./api/ApiKeysSection.jsx')
    render(<ApiKeysSection />)
    await waitFor(() => expect(App.APIKeyList).toHaveBeenCalledTimes(1))
    expect(screen.getByTestId('api-keys-section')).toBeInTheDocument()
    expect(App.APIStatus).not.toHaveBeenCalled()
    expect(App.APIModels).not.toHaveBeenCalled()
    expect(App.APIConfigShow).not.toHaveBeenCalled()
  })

  it('reads the keys again when the profile changes', async () => {
    const { default: ApiKeysSection } = await import('./api/ApiKeysSection.jsx')
    const { rerender } = render(<ApiKeysSection profileId="a" />)
    await waitFor(() => expect(App.APIKeyList).toHaveBeenCalledTimes(1))
    rerender(<ApiKeysSection profileId="b" />)
    await waitFor(() => expect(App.APIKeyList).toHaveBeenCalledTimes(2))
  })

  it('tells the status it read, so that nothing else has to ask for it', async () => {
    const { default: ApiSection } = await import('./ApiSection.jsx')
    const onStatus = vi.fn()
    render(<ApiSection part="machine" onStatus={onStatus} />)
    await waitFor(() => expect(onStatus).toHaveBeenCalledTimes(1))
    expect(onStatus.mock.calls[0][0]).toEqual(statusOf([mainListener()]))
    expect(App.APIStatus).toHaveBeenCalledTimes(1)
  })
})
