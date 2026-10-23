// @vitest-environment jsdom
import React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, cleanup } from '@testing-library/react'
import '../../i18n.js'

const { budget, mockConfirm } = vi.hoisted(() => ({ budget: { lower: vi.fn() }, mockConfirm: vi.fn() }))
vi.mock('../settings/aitaskBudgetApi.js', () => ({ aitaskBudgetApi: budget, money: n => `$${Number(n || 0).toFixed(2)}` }))
vi.mock('../ConfirmDialog.jsx', () => ({ confirm: (...a) => mockConfirm(...a) }))

import { CapLower, checkLower } from './PaneCapLower.jsx'

beforeEach(() => { vi.clearAllMocks(); mockConfirm.mockResolvedValue(true) })
afterEach(cleanup)

const cost = { scope: 'task', capUsd: 2, spentUsd: 0.5 }

describe('checkLower', () => {
  it('accepts a plain decimal strictly between the spend and the cap', () => {
    expect(checkLower('1', 2, 0.5)).toBeNull()
    expect(checkLower('1.25', 2, null)).toBeNull()
    expect(checkLower('.75', 2, 0.5)).toBeNull()
  })
  it.each(['', 'NaN', '1e2', '-1', ' 1', '0x1', 'Infinity'])('refuses %j as not a number', v => {
    expect(checkLower(v, 2, 0.5).key).toBe('bubbles.pane.lowerNotNumber')
  })
  it('refuses equal and higher than the cap, and at or under the spend', () => {
    expect(checkLower('2', 2, 0.5).key).toBe('bubbles.pane.lowerNotBelow')
    expect(checkLower('3', 2, 0.5).key).toBe('bubbles.pane.lowerNotBelow')
    expect(checkLower('0.5', 2, 0.5).key).toBe('bubbles.pane.lowerBelowSpent')
  })
})

describe('CapLower disabled states', () => {
  it('is off, with the reason, while access is unknown', () => {
    render(<CapLower taskId="t1" cost={cost} enabled={false} confined={false} onLowered={() => {}} />)
    expect(screen.getByLabelText('New cap for this chat (USD)')).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Lower cap' })).toBeDisabled()
    expect(screen.getByText(/access level has been confirmed/)).toBeInTheDocument()
    fireEvent.submit(screen.getByTestId('pane-lower'))
    expect(budget.lower).not.toHaveBeenCalled()
  })
  it('renders nothing while the cap is loading, and no control when confined or capless', () => {
    const { container, rerender } = render(<CapLower taskId="t1" cost={undefined} enabled confined={false} onLowered={() => {}} />)
    expect(container).toBeEmptyDOMElement()
    rerender(<CapLower taskId="t1" cost={cost} enabled confined onLowered={() => {}} />)
    expect(screen.queryByTestId('pane-lower')).toBeNull()
    rerender(<CapLower taskId="t1" cost={null} enabled confined={false} onLowered={() => {}} />)
    expect(screen.queryByTestId('pane-lower')).toBeNull()
  })
})
