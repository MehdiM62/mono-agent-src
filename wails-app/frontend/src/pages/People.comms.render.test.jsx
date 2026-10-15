// @vitest-environment jsdom
// People (#479): companies are labelled, our own account is labelled, and a
// person with communications (pending ones counted) links to them.
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, cleanup, fireEvent, within } from '@testing-library/react'
import '../i18n.js'

vi.mock('../services/api.js', () => ({
  api: {
    getPeople: () => Promise.resolve([
      { id: 'p1', username: 'sam', platform: 'LINKEDIN', full_name: 'Sam', kind: 'person' },
      { id: 'c1', username: 'acme', platform: 'LINKEDIN', full_name: 'Acme Inc', kind: 'company' },
      { id: 'me', username: 'mine', platform: 'X', full_name: 'Me', kind: 'person', own_account: true },
    ]),
    getPeopleCount: () => Promise.resolve(3),
    getPeopleTagsMap: () => Promise.resolve({}),
    getAllPersonMessages: () => Promise.resolve([
      { id: 'a', person_id: 'p1', direction: 'outbound', status: 'pending_approval' },
      { id: 'b', person_id: 'p1', direction: 'outbound', status: 'sent' },
    ]),
    openURL: vi.fn(),
  },
  subscribeEvent: () => () => {},
}))
import People from './People.jsx'

afterEach(cleanup)
const rowOf = (u) => screen.getByText(`@${u}`).closest('tr')

describe('People communications', () => {
  it('labels companies and our own account, and links a person to their communications', async () => {
    const onNavigate = vi.fn()
    render(<People onProfile={vi.fn()} onNavigate={onNavigate} />)
    await screen.findByText('@sam')
    expect(within(rowOf('acme')).getByTestId('badge-company')).toHaveTextContent('Company')
    expect(within(rowOf('sam')).queryByTestId('badge-company')).toBeNull()
    expect(within(rowOf('mine')).getByTestId('badge-own')).toHaveTextContent('Your account')
    const link = await within(rowOf('sam')).findByTestId('people-comms-link')
    expect(link).toHaveTextContent('2 communications')
    expect(link).toHaveTextContent('1 pending')
    fireEvent.click(link)
    expect(onNavigate).toHaveBeenCalledWith('communications', { person: 'p1' })
    expect(within(rowOf('acme')).queryByTestId('people-comms-link')).toBeNull()
  })
})
