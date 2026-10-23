// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { fireEvent, screen, cleanup } from '@testing-library/react'
import { createRoot } from 'react-dom/client'
import { flushSync } from 'react-dom'
import '../../i18n.js'
import InfoField from './InfoField.jsx'

vi.mock('../../wailsjs/go/main/App', () => ({ RevealProfileInfo: vi.fn() }))

const field = { key: 'ids.passport', label: 'Passport number', group: 'ids', kind: 'text', sensitive: true }
const entry = { key: 'ids.passport', value: '•••• 4567', masked: true, source: 'user', updated_at: 't' }

afterEach(() => { cleanup(); globalThis.IS_REACT_ACT_ENVIRONMENT = true })

describe('InfoField masked Change', () => {
  // The mount effect that syncs the draft from the stored value can still be
  // pending when the first click lands (a loaded machine). It must not then
  // overwrite the empty draft Change just set.
  it('Change opens an empty input even when the mount effects have not run yet', () => {
    globalThis.IS_REACT_ACT_ENVIRONMENT = false
    const host = document.body.appendChild(document.createElement('div'))
    const root = createRoot(host)
    root.render(<InfoField field={field} entry={entry} onSave={() => {}} onChanged={() => {}} />)
    return new Promise(resolve => {
      const poll = () => {
        const btn = host.querySelector('button.info-btn')
        if (!btn) return setTimeout(poll, 0)
        flushSync(() => { btn.click() })
        resolve()
      }
      poll()
    }).then(() => new Promise(r => setTimeout(r, 50))).then(() => {
      expect(screen.getByLabelText('Passport number')).toHaveValue('')
      root.unmount()
    })
  })
})
