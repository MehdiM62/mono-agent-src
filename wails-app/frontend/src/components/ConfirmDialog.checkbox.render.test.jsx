// @vitest-environment jsdom
import { describe, it, expect, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, cleanup, act } from '@testing-library/react'
import ConfirmHost, { confirm } from './ConfirmDialog.jsx'

afterEach(cleanup)

describe('confirm with a checkbox', () => {
  it('shows the label and the red warning, unchecked, and resolves { checked }', async () => {
    render(<ConfirmHost />)
    let p
    act(() => { p = confirm('Delete?', { confirmLabel: 'Delete', checkbox: { label: 'Also delete the file', warning: 'Permanent.' } }) })
    const box = screen.getByLabelText('Also delete the file')
    expect(box.checked).toBe(false)
    expect(screen.getByText('Permanent.')).toBeInTheDocument()
    fireEvent.click(box)
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(await p).toEqual({ checked: true })
  })

  it('cancelling resolves false, and a plain confirm still resolves true', async () => {
    render(<ConfirmHost />)
    let p
    act(() => { p = confirm('Delete?', { checkbox: { label: 'x' } }) })
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(await p).toBe(false)
    act(() => { p = confirm('Plain?') })
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }))
    expect(await p).toBe(true)
  })
})
