// @vitest-environment jsdom
// The #872 review's second round on the cards themselves: MonoAgent's own
// secret commands are hidden like vault commands (output too), the generic
// card follows the vault rule, and token shapes in any output read •••, its
// copy included. The values are fakes.
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, cleanup, within } from '@testing-library/react'
import '../../i18n.js'

vi.mock('../../services/hilApi.js', () => ({ listAsks: vi.fn(async () => []), resolve: vi.fn() }))

import { ChatTimeline } from './ChatTimeline.jsx'
import { chatReducer, initialChatState } from './chatReducer.js'
import { ToolActivityCard } from './ToolActivityCard.jsx'
import { replayStage } from '../../lib/orgStage.js'
import journal from '../../lib/__fixtures__/orgStageJournal.json'

const S = 'Zq8Xfake4417'
const TOKEN = ['sk', 'fakefakefakefakefake1234'].join('-')
const MASK = '•••'

let writeText
beforeEach(() => {
  writeText = vi.fn()
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
})
afterEach(cleanup)

let seq = 0
const ev = (type, payload) => ({ type: 'event', event: { conversationId: 'c1', turnId: 't1', seq: ++seq, at: '2026-10-19T10:00:00.000Z', type, payload } })
function card(command, result) {
  const state = [
    ev('tool.started', { callId: 'c1', name: 'Bash', native: true, kind: 'shell', arguments: { command } }),
    ev('tool.completed', { callId: 'c1', ok: true, result }),
  ].reduce(chatReducer, initialChatState())
  render(<ChatTimeline state={state} turnId="t1" isLive={false} />)
  const el = screen.getByTestId('native-tool-card')
  fireEvent.click(within(el).getByRole('button', { expanded: false }))
  return el
}

describe("MonoAgent's own secret commands", () => {
  it.each([
    [`monoagentcli profile info set card_number ${S}`, `saved card_number ${S}`],
    ['monoagentcli profile info get bank_iban --reveal', `bank_iban: ${S}`],
    ['monoagentcli account login --email me@x.test --code 123456', 'signed in'],
    [`monoagentcli jev key set ${S}`, 'saved'],
    [`monoagentcli connect set-oauth-client outlook --client-secret ${S}`, 'saved'],
    ['monoagentcli api key create --name ci', `key: mak_${S}`],
  ])('%s shows neither its command nor its output', (command, result) => {
    const el = card(command, result)
    expect(el).toHaveTextContent('Vault-related command (hidden)')
    expect(within(el).getByTestId('vault-hidden')).toBeInTheDocument()
    expect(document.body.innerHTML).not.toContain(S)
    expect(el).not.toHaveTextContent('123456')
    expect(el).not.toHaveTextContent('mak_')
  })
})

describe('the generic card', () => {
  it('follows the vault rule: no arguments, no result', () => {
    const call = { callId: 'g1', name: 'run_shell', native: false, arguments: { command: `monoagentcli secret add --name t --value ${S}` }, status: 'completed', ok: true, result: `stored ${S}` }
    render(<ToolActivityCard call={call} turnId="t1" isLive={false} />)
    fireEvent.click(screen.getByRole('button', { expanded: false }))
    expect(screen.getByTestId('vault-hidden')).toBeInTheDocument()
    expect(document.body.innerHTML).not.toContain(S)
    expect(screen.queryByRole('button', { name: 'Copy result' })).toBeNull()
  })

  it('masks a token shape in its result, the copy included', () => {
    const call = { callId: 'g2', name: 'read_env', native: false, arguments: { file: '.env' }, status: 'completed', ok: true, result: `OPENAI_API_KEY=${TOKEN}` }
    render(<ToolActivityCard call={call} turnId="t1" isLive={false} />)
    fireEvent.click(screen.getByRole('button', { expanded: false }))
    expect(document.body).toHaveTextContent(`OPENAI_API_KEY=${MASK}`)
    fireEvent.click(screen.getByRole('button', { name: 'Copy result' }))
    expect(writeText).toHaveBeenCalledWith(`OPENAI_API_KEY=${MASK}`)
    expect(document.body.innerHTML).not.toContain(TOKEN)
  })
})

describe('output on a native card', () => {
  it.each([
    ['cat .env', `OPENAI_API_KEY=${TOKEN}\nDEBUG=1`],
    ['env', `PATH=/usr/bin\nGH_TOKEN=${['ghp', 'fakefakefakefakefakefake12'].join('_')}`],
    ['gh auth token', ['gho', 'fakefakefakefakefakefake12'].join('_')],
  ])('%s: a token shape in the output reads •••, in its copy too', (command, result) => {
    const el = card(command, result)
    expect(el).toHaveTextContent(MASK)
    fireEvent.click(within(el).getByRole('button', { name: 'Copy output' }))
    expect(writeText).toHaveBeenCalledTimes(1)
    expect(writeText.mock.calls[0][0]).toContain(MASK)
    for (const text of [document.body.innerHTML, writeText.mock.calls[0][0]]) {
      expect(text).not.toMatch(/sk-fake|ghp_fake|gho_fake/)
    }
  })
})

describe("a native subagent's result on the stage", () => {
  it('masks a token shape in its summary and its flight', () => {
    const done = journal.find(e => e.type === 'tool.completed' && e.payload.result === 'The test sleeps 10ms and expects expiry.')
    const stage = replayStage(journal.map(e => (e === done ? { ...e, payload: { ...e.payload, result: `Use ${TOKEN} for it.` } } : e)))
    expect(stage.nodes['native:w1:t1'].summary).toBe(`Use ${MASK} for it.`)
    expect(JSON.stringify(stage)).not.toContain(TOKEN)
  })
})
