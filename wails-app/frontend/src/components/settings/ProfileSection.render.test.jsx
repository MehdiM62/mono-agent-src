// @vitest-environment jsdom
// The Profile section of Settings: rename, icon, and the three-step delete.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, cleanup, fireEvent, waitFor, act } from '@testing-library/react'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (k, o) => (o?.defaultValue ?? (o ? `${k}${JSON.stringify(o)}` : k)) }),
}))
vi.mock('../orgdesigner/IconPickerModal.jsx', () => ({ default: () => null }))
vi.mock('../../wailsjs/go/main/App', () => ({
  GetProfiles: vi.fn(),
  UpdateProfile: vi.fn(),
  GetProfileDeleteSummary: vi.fn(),
  DeleteProfile: vi.fn(),
}))
import * as App from '../../wailsjs/go/main/App'
import ProfileSection from './ProfileSection.jsx'

const profile = { id: 'p1', name: 'Work', icon: '' }
const settle = () => act(() => new Promise(r => setTimeout(r, 650)))

beforeEach(() => {
  App.GetProfiles.mockResolvedValue([{ id: 'p1' }, { id: 'p2' }])
  App.GetProfileDeleteSummary.mockResolvedValue({ tables: [{ table: 'person_messages', rows: 3 }, { table: 'tasks', rows: 2 }], folder: '/f' })
  App.DeleteProfile.mockResolvedValue('/backups/pre-delete-profile-1.db')
  App.UpdateProfile.mockResolvedValue({})
})
afterEach(() => { cleanup(); vi.clearAllMocks() })

async function openDialog() {
  render(<ProfileSection profile={profile} />)
  await waitFor(() => expect(App.GetProfiles).toHaveBeenCalled())
  fireEvent.click(screen.getByText('settings.profile.deleteButton'))
  await screen.findByText('person messages')
}

describe('ProfileSection', () => {
  it('disables Delete for the only profile and says why', async () => {
    App.GetProfiles.mockResolvedValue([{ id: 'p1' }])
    render(<ProfileSection profile={profile} />)
    expect(await screen.findByText('settings.profile.onlyProfile')).toBeInTheDocument()
    expect(screen.getByText('settings.profile.deleteButton').closest('button')).toBeDisabled()
  })

  it('re-reads the profile count when Settings becomes active again', async () => {
    App.GetProfiles.mockResolvedValue([{ id: 'p1' }])
    const { rerender } = render(<ProfileSection profile={profile} isActive />)
    expect(await screen.findByText('settings.profile.onlyProfile')).toBeInTheDocument()
    rerender(<ProfileSection profile={profile} isActive={false} />)
    App.GetProfiles.mockResolvedValue([{ id: 'p1' }, { id: 'p2' }])
    rerender(<ProfileSection profile={profile} isActive />)
    await waitFor(() => expect(screen.queryByText('settings.profile.onlyProfile')).not.toBeInTheDocument())
    expect(screen.getByText('settings.profile.deleteButton').closest('button')).not.toBeDisabled()
  })

  it('renames through UpdateProfile and shows the error when it fails', async () => {
    App.UpdateProfile.mockRejectedValue(new Error('a profile with that name exists'))
    render(<ProfileSection profile={profile} />)
    fireEvent.change(screen.getByLabelText('settings.profile.nameLabel'), { target: { value: 'Office' } })
    fireEvent.click(screen.getByText('settings.profile.rename'))
    expect(await screen.findByRole('alert')).toHaveTextContent('a profile with that name exists')
    expect(App.UpdateProfile).toHaveBeenCalledWith('p1', 'Office', '')
  })

  it('lists what is lost, then needs a ticked box, then the typed name', async () => {
    await openDialog()
    await settle()
    fireEvent.click(screen.getByText('settings.profile.step1Continue'))
    const cont = await screen.findByText('settings.profile.step2Continue')
    expect(cont.closest('button')).toBeDisabled()
    await settle()
    fireEvent.click(screen.getByLabelText('settings.profile.step2Ack'))
    fireEvent.click(cont)
    const input = await screen.findByLabelText('Work')
    const del = screen.getByText('settings.profile.delete').closest('button')
    expect(del).toBeDisabled()
    fireEvent.change(input, { target: { value: 'work' } })
    expect(del).toBeDisabled()
    fireEvent.change(input, { target: { value: 'Work' } })
    expect(del).not.toBeDisabled()
    await settle()
    fireEvent.click(del)
    await waitFor(() => expect(App.DeleteProfile).toHaveBeenCalledWith('p1'))
    expect(await screen.findByText(/pre-delete-profile-1\.db/)).toBeInTheDocument()
  })

  it('ignores a click that arrives right after the step changed', async () => {
    await openDialog()
    await settle()
    const cont = screen.getByText('settings.profile.step1Continue')
    fireEvent.click(cont)
    fireEvent.click(cont) // a double-click
    await screen.findByText('settings.profile.step2Ack')
    expect(screen.queryByText(/step3Intro/)).toBeNull()
  })

  it('keeps the dialog open and shows the error when the delete fails', async () => {
    App.DeleteProfile.mockRejectedValue(new Error('the profile has 1 running or queued workflow execution(s)'))
    await openDialog()
    await settle()
    fireEvent.click(screen.getByText('settings.profile.step1Continue'))
    await screen.findByText('settings.profile.step2Ack')
    await settle()
    fireEvent.click(screen.getByLabelText('settings.profile.step2Ack'))
    fireEvent.click(screen.getByText('settings.profile.step2Continue'))
    fireEvent.change(await screen.findByLabelText('Work'), { target: { value: 'Work' } })
    await settle()
    fireEvent.click(screen.getByText('settings.profile.delete'))
    expect(await screen.findByRole('alert')).toHaveTextContent('running or queued')
    expect(screen.queryByText('settings.profile.doneTitle')).toBeNull()
  })
})
