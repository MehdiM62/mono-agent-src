// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, waitFor, within, cleanup } from '@testing-library/react'
import '../../i18n.js'
import ConfirmHost from '../ConfirmDialog.jsx'
import CoderOrgSection from './CoderOrgSection.jsx'

// Each control calls its binding (and so `monoagentcli coder set` / `hil
// allow …`) with the right arguments.

const App = {}
beforeEach(() => {
  for (const k of ['CoderStatus', 'CoderSet', 'HilSendSetApproval', 'HilSendAllowList', 'HilSendAllowAdd', 'HilSendAllowRemove']) App[k] = vi.fn()
  window.go = { main: { App } }
})
afterEach(() => { cleanup(); delete window.go })

const status = (over = {}) => JSON.stringify({ enabled: true, orgRouter: 'auto', sendApproval: true, ...over })
const rules = [
  { id: 'r1', action: 'email', platform: 'outlook', account: 'me@acme.test', recipient: 'sam@acme.test' },
  { id: 'r2', action: 'dm', platform: 'linkedin', account: '', recipient: 'https://www.linkedin.com/in/sam-rivera' },
]

function machine(st = status()) {
  App.CoderStatus.mockResolvedValue(st)
  return render(<><CoderOrgSection part="machine" /><ConfirmHost /></>)
}

describe('CoderOrgSection, this computer', () => {
  it('sets the org router with `coder set --org-router`', async () => {
    machine()
    const router = await screen.findByRole('combobox', { name: "Who picks a worker's role" })
    expect(router).toHaveValue('auto')
    expect(within(router).getAllByRole('option').map(o => o.value)).toEqual(['auto', 'jev', 'model'])
    App.CoderSet.mockResolvedValue(status({ orgRouter: 'model' }))
    fireEvent.change(router, { target: { value: 'model' } })
    // Only the router changes: the other settings are passed as "leave alone".
    await waitFor(() => expect(App.CoderSet).toHaveBeenCalledWith('', 0, '', -1, 'model'))
    await waitFor(() => expect(router).toHaveValue('model'))
    expect(screen.getByText(/The lead names roles from its org roster/)).toBeInTheDocument()
  })

  it('turning send approval off asks first; cancelling changes nothing', async () => {
    machine()
    const box = await screen.findByRole('checkbox', { name: "Agents' sends wait for my approval" })
    expect(box).toBeChecked()
    fireEvent.click(box)
    const dialog = await screen.findByRole('dialog', { name: 'Let agents send without asking?' })
    expect(dialog).toHaveTextContent('Agents could send email, direct messages, comments and posts, and publish, as you, without asking you first.')
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(App.HilSendSetApproval).not.toHaveBeenCalled()
    expect(box).toBeChecked()
  })

  it('confirming turns send approval off; turning it on needs no confirmation', async () => {
    machine()
    const box = await screen.findByRole('checkbox', { name: "Agents' sends wait for my approval" })
    App.HilSendSetApproval.mockResolvedValue(status({ sendApproval: false }))
    fireEvent.click(box)
    fireEvent.click(await screen.findByRole('button', { name: 'Let agents send' }))
    await waitFor(() => expect(App.HilSendSetApproval).toHaveBeenCalledWith(false))
    await waitFor(() => expect(box).not.toBeChecked())
    App.HilSendSetApproval.mockResolvedValue(status({ sendApproval: true }))
    fireEvent.click(box)
    await waitFor(() => expect(App.HilSendSetApproval).toHaveBeenLastCalledWith(true))
    expect(screen.queryByRole('dialog')).toBeNull()
    await waitFor(() => expect(box).toBeChecked())
  })

  it('shows the CLI refusal', async () => {
    machine()
    const router = await screen.findByRole('combobox', { name: "Who picks a worker's role" })
    App.CoderSet.mockResolvedValue(JSON.stringify({ error: 'only the operator may change coder mode', code: 'operator_only' }))
    fireEvent.change(router, { target: { value: 'jev' } })
    expect(await screen.findByRole('alert')).toHaveTextContent('only the operator may change coder mode')
  })
})

describe('CoderOrgSection, the profile', () => {
  it('lists the always-allow rules, adds one and removes one', async () => {
    App.HilSendAllowList.mockResolvedValue(JSON.stringify(rules))
    App.HilSendAllowAdd.mockResolvedValue(JSON.stringify({ action: 'comment', platform: 'x', account: '@me', recipient: '@sam' }))
    App.HilSendAllowRemove.mockResolvedValue(JSON.stringify({ removed: 'r1' }))
    render(<CoderOrgSection part="profile" />)
    const first = await screen.findByText('sam@acme.test')
    expect(first.closest('[data-rule]')).toHaveTextContent('Email · outlook · me@acme.test')
    expect(document.querySelector('[data-rule="r2"]')).toHaveTextContent('Direct message · linkedin · —')

    fireEvent.change(screen.getByRole('combobox', { name: 'Action' }), { target: { value: 'comment' } })
    fireEvent.change(screen.getByRole('textbox', { name: 'Platform' }), { target: { value: ' x ' } })
    fireEvent.change(screen.getByRole('textbox', { name: 'From account' }), { target: { value: '@me' } })
    fireEvent.change(screen.getByRole('textbox', { name: 'To' }), { target: { value: '@sam' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    await waitFor(() => expect(App.HilSendAllowAdd).toHaveBeenCalledWith('comment', 'x', '@me', '@sam'))
    await waitFor(() => expect(screen.getByRole('textbox', { name: 'To' })).toHaveValue(''))

    fireEvent.click(screen.getByRole('button', { name: 'Stop always allowing sam@acme.test' }))
    await waitFor(() => expect(App.HilSendAllowRemove).toHaveBeenCalledWith('r1'))
    expect(App.HilSendAllowList).toHaveBeenCalledTimes(3)
  })

  it('says when nothing is always allowed, and needs a recipient to add', async () => {
    App.HilSendAllowList.mockResolvedValue('[]')
    render(<CoderOrgSection part="profile" />)
    expect(await screen.findByText('No destination is always allowed.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add' })).toBeDisabled()
  })
})
