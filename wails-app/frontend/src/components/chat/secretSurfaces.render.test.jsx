// @vitest-environment jsdom
// Every surface that shows a call's command line or input (#872 review): a
// command that may touch the vault reads as hidden, and a credential-shaped
// value reads •••. The values here are fakes.
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, cleanup, within } from '@testing-library/react'
import i18n from '../../i18n.js'

vi.mock('../../services/hilApi.js', () => ({ listAsks: vi.fn(async () => []), resolve: vi.fn() }))
vi.mock('../../services/api.js', () => ({ api: { coderStopBackground: vi.fn() } }))

import { ChatTimeline } from './ChatTimeline.jsx'
import { chatReducer, initialChatState } from './chatReducer.js'
import { ToolActivityCard } from './ToolActivityCard.jsx'
import { CoderBackgroundBanner } from './CoderBackgroundBanner.jsx'
import { OrgChatView } from '../bubbles/OrgChatView.jsx'
import { useDoingText } from '../stage/StageNode.jsx'
import { doingOf } from '../../lib/orgStage.js'
import { nowDoing } from '../../lib/coderBubbles.js'
import { toolSummary } from '../../lib/orgBubble.js'

afterEach(cleanup)

const VAULT = 'monoagentcli secret add --value S3CRETVALUE'
const INPUT = `monoagentcli workflow run abc --input '{"api_key":"K3YVALUE"}'`
const FLAGS = 'deploy --token=T0KENVALUE && API_KEY=ENVVALUE1 node x.js'
const leaks = /S3CRETVALUE|K3YVALUE|T0KENVALUE|ENVVALUE1/

let seq = 0
const ev = (type, payload) => ({ type: 'event', event: { conversationId: 'c1', turnId: 't1', seq: ++seq, at: '2026-10-19T10:00:00.000Z', type, payload } })
function shellTurn(args) {
  return [
    ev('tool.started', { callId: 'c1', name: 'Bash', native: true, kind: 'shell', arguments: args }),
    ev('tool.completed', { callId: 'c1', ok: true, result: 'done' }),
  ].reduce(chatReducer, initialChatState())
}

describe("the stage's running label", () => {
  function Doing({ node }) {
    const doingText = useDoingText()
    return <span data-testid="doing">{doingText(node)}</span>
  }
  const node = (args) => ({ doing: { ...doingOf('Bash', 'shell', args), active: true } })

  it('says a vault command is hidden', () => {
    expect(doingOf('Bash', 'shell', { command: VAULT })).toEqual({ kind: 'shell', name: 'Bash', target: '', vault: true })
    render(<Doing node={node({ command: VAULT })} />)
    expect(screen.getByTestId('doing')).toHaveTextContent('running a vault command (hidden)')
    expect(document.body).not.toHaveTextContent(leaks)
  })

  it('masks a credential in the command', () => {
    expect(doingOf('Bash', 'shell', { command: 'deploy --token=T0KENVALUE' }).target).toBe('deploy --token=•••')
    expect(doingOf('Task', 'task', { description: 'use API_KEY=ENVVALUE1' }).target).toBe('use API_KEY=•••')
  })
})

describe("the coder bubble's activity line", () => {
  const turn = (args) => ({ calls: { a: { name: 'Bash', status: 'started', arguments: args } } })
  it('names no target for a vault command, and masks a credential', () => {
    expect(nowDoing(turn({ command: VAULT }), k => i18n.t(k))).toBe('Bash · Vault-related command (hidden)')
    expect(nowDoing(turn(JSON.stringify({ command: 'bash -c "monoagentcli secret get t"' })), k => i18n.t(k))).toBe('Bash · Vault-related command (hidden)')
    expect(nowDoing(turn({ command: 'deploy --token=T0KENVALUE' }))).toBe('Bash · deploy --token=•••')
  })
})

describe('the background-process banner', () => {
  it('hides a vault command and masks a credential', () => {
    const notice = { code: 'coder.background', message: '', processes: [{ pid: 11, command: VAULT }, { pid: 12, command: FLAGS }] }
    render(<CoderBackgroundBanner notice={notice} />)
    const list = screen.getByTestId('coder-background-processes')
    expect(list).toHaveTextContent('Vault-related command (hidden)')
    expect(list).toHaveTextContent('deploy --token=••• && API_KEY=••• node x.js')
    expect(document.body.innerHTML).not.toMatch(leaks) // the title attributes too
  })
})

describe('the tool cards', () => {
  it("mask the probe's --input JSON on a workflow card, its copy included", () => {
    render(<ChatTimeline state={shellTurn({ command: INPUT })} turnId="t1" isLive={false} onNavigate={vi.fn()} />)
    const card = screen.getByTestId('native-tool-card')
    fireEvent.click(within(card).getByRole('button', { expanded: false }))
    expect(card).toHaveTextContent(`--input '{"api_key":"•••"}'`)
    expect(document.body.innerHTML).not.toMatch(leaks)
  })

  it('mask the flag and env forms on a plain shell card', () => {
    render(<ChatTimeline state={shellTurn({ command: FLAGS })} turnId="t1" isLive={false} />)
    expect(screen.getByTestId('native-tool-card')).toHaveTextContent('$ deploy --token=••• && API_KEY=••• node x.js')
    expect(document.body.innerHTML).not.toMatch(leaks)
  })

  it('mask the raw input of arguments cut short', () => {
    render(<ChatTimeline state={shellTurn(`{"command":"curl -H 'Authorization: Bearer K3YVALUE' x and more`)} turnId="t1" isLive={false} />)
    const card = screen.getByTestId('native-tool-card')
    fireEvent.click(within(card).getByRole('button', { expanded: false }))
    expect(card).toHaveTextContent('Input (truncated)')
    expect(card).toHaveTextContent('Authorization: Bearer •••')
    expect(document.body.innerHTML).not.toMatch(leaks)
  })

  it("mask a generic card's arguments", () => {
    const call = { callId: 'g1', name: 'http_request', native: false, arguments: { url: 'https://x.test/?access_token=T0KENVALUE', headers: { Authorization: 'Bearer K3YVALUE' } }, status: 'completed', ok: true, result: 'ok' }
    render(<ToolActivityCard call={call} turnId="t1" isLive={false} />)
    fireEvent.click(screen.getByRole('button', { expanded: false }))
    expect(document.body).toHaveTextContent('access_token=•••')
    expect(document.body.innerHTML).not.toMatch(leaks)
  })
})

describe("an org bubble's tool rows", () => {
  it('mask a credential in the summary', () => {
    expect(toolSummary({ name: 'Bash', arguments: { command: INPUT } })).toBe(`Bash · monoagentcli workflow run abc --input '{"api_key":"•••"}'`)
    const calls = { a: { callId: 'a', name: 'Bash', arguments: { command: FLAGS }, status: 'completed', ok: true, startedAt: '2026-10-19T10:00:00Z' } }
    const conv = {
      history: { items: [] }, historyError: '', items: [], echoes: [], running: true, sending: false, sendError: '', busy: {}, outcome: {},
      bubble: { calls, callIds: { dev: ['a'] } }, send: vi.fn(), resolveItem: vi.fn(),
    }
    render(<OrgChatView conv={conv} nameOf={id => id} draft="" onDraftChange={vi.fn()} orgName="growth" view={{ mode: 'role', role: 'dev' }} onView={vi.fn()} onNavigate={vi.fn()} />)
    expect(screen.getByTestId('org-chat-tool')).toHaveTextContent('deploy --token=••• && API_KEY=•••')
    expect(document.body.innerHTML).not.toMatch(leaks)
  })
})
