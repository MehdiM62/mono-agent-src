// @vitest-environment jsdom
// Communications (#479): a status badge per outbound row, a status filter,
// a pending row with its approval action, and a refresh when a HIL decision
// is made elsewhere.
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, cleanup, fireEvent, waitFor, within, act } from '@testing-library/react'
import '../i18n.js'
import { STATUSES, emitCommsChanged } from '../lib/commsStatus.js'

const rows = STATUSES.map(s => ({
  id: `m-${s}`, person_id: 'p1', direction: 'outbound', source: 'linkedin', subject: `subj ${s}`,
  status: s, hil_ref: s === 'pending_approval' ? 'hil-9' : '', person_platform_username: 'sam',
}))
rows.push({ id: 'old', person_id: 'p1', direction: 'outbound', source: 'linkedin', subject: 'legacy', person_platform_username: 'sam' })
rows.push({ id: 'in', person_id: 'p1', direction: 'inbound', source: 'linkedin', subject: 'inbound one', person_platform_username: 'sam' })

const getAll = vi.fn()
vi.mock('../services/api.js', () => ({
  api: { getAllPersonMessages: (...a) => getAll(...a), markPersonMessagesRead: vi.fn(() => Promise.resolve(null)) },
}))
vi.mock('../components/MessageDetailModal.jsx', () => ({ default: () => null }))
import Communications from './Communications.jsx'

afterEach(() => { cleanup(); getAll.mockReset() })
const rowOf = (subject) => screen.getByText(subject).closest('[role="button"]')

describe('Communications status', () => {
  it('shows a text badge per status, old rows as Sent, none on inbound', async () => {
    getAll.mockResolvedValue(rows)
    render(<Communications onProfile={vi.fn()} />)
    await screen.findByText('subj sent')
    const want = { draft: 'Draft', pending_approval: 'Pending approval', approved: 'Approved', sending: 'Sending', sent: 'Sent', rejected: 'Rejected', failed: 'Failed', expired: 'Expired' }
    for (const s of STATUSES) {
      const b = within(rowOf(`subj ${s}`)).getByTestId('comms-status')
      expect(b).toHaveAttribute('data-status', s)
      expect(b).toHaveTextContent(want[s])
    }
    expect(within(rowOf('legacy')).getByTestId('comms-status')).toHaveAttribute('data-status', 'sent')
    expect(within(rowOf('inbound one')).queryByTestId('comms-status')).toBeNull()
  })

  it('shows every row by default and narrows with the status filter', async () => {
    getAll.mockResolvedValue(rows)
    render(<Communications onProfile={vi.fn()} />)
    await screen.findByText('subj sent')
    expect(screen.getByText('subj pending_approval')).toBeInTheDocument()
    fireEvent.click(screen.getByTestId('status-filter-pending_approval'))
    expect(screen.getByText('subj pending_approval')).toBeInTheDocument()
    expect(screen.queryByText('subj sent')).toBeNull()
    fireEvent.click(screen.getByTestId('status-filter-approved'))
    expect(screen.getByText('subj approved')).toBeInTheDocument()
    expect(screen.getByText('subj sending')).toBeInTheDocument()
    fireEvent.click(screen.getByTestId('status-filter-failed'))
    expect(screen.getByText('subj failed')).toBeInTheDocument()
    expect(screen.queryByText('subj approved')).toBeNull()
    fireEvent.click(screen.getByTestId('status-filter-all'))
    expect(screen.getByText('subj sent')).toBeInTheDocument()
  })

  it('a pending row says it waits and its button opens the approval card', async () => {
    getAll.mockResolvedValue(rows)
    const onOpenHil = vi.fn()
    render(<Communications onProfile={vi.fn()} onOpenHil={onOpenHil} />)
    await screen.findByText('subj pending_approval')
    const row = rowOf('subj pending_approval')
    expect(within(row).getByText('Waiting for your approval')).toBeInTheDocument()
    fireEvent.click(within(row).getByTestId('comms-review'))
    expect(onOpenHil).toHaveBeenCalledWith('hil-9')
    expect(within(rowOf('subj sent')).queryByText('Waiting for your approval')).toBeNull()
  })

  it('highlights the row a "See in Communications" link names', async () => {
    getAll.mockResolvedValue(rows)
    render(<Communications onProfile={vi.fn()} navData={{ hil_ref: 'hil-9' }} />)
    await screen.findByText('subj pending_approval')
    expect(rowOf('subj pending_approval')).toHaveAttribute('data-focused', 'true')
    expect(rowOf('subj sent')).not.toHaveAttribute('data-focused')
  })

  it("a person's link narrows to their messages and can be cleared", async () => {
    getAll.mockResolvedValue([...rows, { id: 'o', person_id: 'p2', direction: 'outbound', source: 'x', subject: 'other person', status: 'sent' }])
    render(<Communications onProfile={vi.fn()} navData={{ person: 'p2' }} />)
    await screen.findByText('other person')
    expect(screen.queryByText('subj sent')).toBeNull()
    fireEvent.click(screen.getByTestId('clear-person-filter'))
    expect(screen.getByText('subj sent')).toBeInTheDocument()
  })

  it('reloads when a HIL decision is made elsewhere', async () => {
    getAll.mockResolvedValue(rows)
    render(<Communications onProfile={vi.fn()} />)
    await screen.findByText('subj sent')
    expect(getAll).toHaveBeenCalledTimes(1)
    getAll.mockResolvedValue(rows.map(r => (r.id === 'm-pending_approval' ? { ...r, status: 'approved' } : r)))
    act(() => emitCommsChanged())
    await waitFor(() => expect(within(rowOf('subj pending_approval')).getByTestId('comms-status')).toHaveAttribute('data-status', 'approved'))
    expect(getAll).toHaveBeenCalledTimes(2)
  })
})
