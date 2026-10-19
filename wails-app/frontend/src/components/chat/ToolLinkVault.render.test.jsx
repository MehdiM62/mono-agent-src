// @vitest-environment jsdom
// The #872 review's probe, rendered: a vault command anywhere in a command
// line, wrapped in a shell, behind a variable or in arguments cut short never
// shows its command line, a value or its output on a tool card or an org
// bubble's tool row. The values here are fakes.
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, cleanup, within } from '@testing-library/react'
import '../../i18n.js'

vi.mock('../../services/hilApi.js', () => ({ listAsks: vi.fn(async () => []), resolve: vi.fn() }))

import { ChatTimeline } from './ChatTimeline.jsx'
import { chatReducer, initialChatState } from './chatReducer.js'
import { OrgChatView } from '../bubbles/OrgChatView.jsx'

afterEach(cleanup)

let seq = 0
const ev = (type, payload) => ({ type: 'event', event: { conversationId: 'c1', turnId: 't1', seq: ++seq, at: '2026-10-19T10:00:00.000Z', type, payload } })
function turn(callId, args, result) {
  return [
    ev('tool.started', { callId, name: 'Bash', native: true, kind: 'shell', arguments: args }),
    ev('tool.completed', { callId, ok: true, result }),
  ].reduce(chatReducer, initialChatState())
}
const OUTPUT = 'GITHUB_TOKEN=ghp_FAKEOUTPUT'
// Everything a card may show, the opened card included.
function shown(args) {
  render(<ChatTimeline state={turn('c1', args, OUTPUT)} turnId="t1" isLive={false} onNavigate={vi.fn()} />)
  const card = screen.getByTestId('native-tool-card')
  fireEvent.click(within(card).getByRole('button', { expanded: false }))
  return card
}

describe('a vault command in a command line', () => {
  it.each([
    ['monoagentcli task list && monoagentcli secret reveal GITHUB_TOKEN --reveal', 'Vault · reveal “GITHUB_TOKEN”'],
    ['monoagentcli task list; monoagentcli secret add --name t --value SUPERSECRET', 'Vault · add'],
    ['echo "$(monoagentcli secret add --name t --value SUPERSECRET)"', 'Vault · add'],
    ['env X=1 monoagentcli secret add --name t --value=SUPERSECRET', 'Vault · add'],
    ['/usr/local/bin/monoagentcli secret update tok --field token=SUPERSECRET', 'Vault · update “tok”'],
    ['monoagentcli secret list && cat ~/.ssh/id_rsa', 'Vault · list'],
  ])('%s is a vault card: the name only', (command, title) => {
    const card = shown({ command })
    expect(card).toHaveTextContent(title)
    expect(card).not.toHaveTextContent('SUPERSECRET')
    expect(card).not.toHaveTextContent('FAKEOUTPUT')
    expect(card).not.toHaveTextContent('task list')
    expect(within(card).getByTestId('vault-hidden')).toBeInTheDocument()
  })
})

describe('a vault command the card cannot read', () => {
  it.each([
    'bash -c "monoagentcli secret add --name t --value SUPERSECRET"',
    "sh -c 'monoagentcli secret add --name t --value SUPERSECRET'",
    'CLI=monoagentcli; $CLI secret add --name t --value SUPERSECRET',
    'eval "monoagentcli secret add --name t --value SUPERSECRET"',
    "monoagent'cli' secret add --name t --value SUPERSECRET",
  ])('%s shows a neutral card', (command) => {
    const card = shown({ command, description: 'store SUPERSECRET' })
    expect(card).toHaveTextContent('Vault-related command (hidden)')
    expect(card).not.toHaveTextContent('SUPERSECRET')
    expect(card).not.toHaveTextContent('FAKEOUTPUT')
    expect(card).not.toHaveTextContent('--value')
    expect(within(card).getByTestId('vault-hidden')).toBeInTheDocument()
  })

  it('withholds raw arguments cut short when they hold a vault command', () => {
    const card = shown('{"command":"monoagentcli secret add --name t --value SUPERSECRET and more')
    expect(card).not.toHaveTextContent('SUPERSECRET')
    expect(card).not.toHaveTextContent('Input (truncated)')
    expect(within(card).getByTestId('vault-hidden')).toBeInTheDocument()
  })

  it('still shows raw arguments cut short when they hold no vault command', () => {
    const card = shown('{"command":"go test ./internal/... and more')
    expect(card).toHaveTextContent('Input (truncated)')
    expect(card).toHaveTextContent('go test ./internal/...')
  })
})

describe('a vault import', () => {
  it('names neither the file nor its folder', () => {
    const card = shown({ command: 'monoagentcli secret import /home/user/private/vault-export.json.enc' })
    expect(card).toHaveTextContent('Vault · import')
    expect(card).not.toHaveTextContent('vault-export')
    expect(card).not.toHaveTextContent('/home/user/private')
  })
})

describe('a command named like an object property', () => {
  it('reads as a plain shell command', () => {
    const card = shown({ command: 'monoagentcli toString foo' })
    expect(card).toHaveTextContent('$ monoagentcli toString foo')
    expect(card).not.toHaveTextContent('undefined')
    expect(screen.queryByTestId('tool-link')).toBeNull()
  })
})

describe("an org bubble's tool rows", () => {
  it('name a chained or wrapped vault call without its command line', () => {
    const calls = {
      a: { callId: 'a', name: 'Bash', arguments: { command: 'monoagentcli task list; monoagentcli secret add --name t --value SUPERSECRET' }, status: 'completed', ok: true, startedAt: '2026-10-19T10:00:00Z' },
      b: { callId: 'b', name: 'Bash', arguments: { command: 'bash -c "monoagentcli secret add --value SUPERSECRET"' }, status: 'completed', ok: true, startedAt: '2026-10-19T10:00:01Z' },
      c: { callId: 'c', name: 'Bash', arguments: '{"command":"monoagentcli secret add --value SUPERSECRET', status: 'completed', ok: true, startedAt: '2026-10-19T10:00:02Z' },
    }
    const conv = {
      history: { items: [] }, historyError: '', items: [], echoes: [], running: true, sending: false, sendError: '', busy: {}, outcome: {},
      bubble: { calls, callIds: { dev: ['a', 'b', 'c'] } }, send: vi.fn(), resolveItem: vi.fn(),
    }
    render(<OrgChatView conv={conv} nameOf={id => id} draft="" onDraftChange={vi.fn()} orgName="growth" view={{ mode: 'role', role: 'dev' }} onView={vi.fn()} onNavigate={vi.fn()} />)
    const rows = screen.getAllByTestId('org-chat-tool')
    expect(rows[0]).toHaveTextContent('Vault · add')
    expect(rows[1]).toHaveTextContent('Vault-related command (hidden)')
    expect(rows[2]).toHaveTextContent('Vault-related command (hidden)')
    expect(document.body).not.toHaveTextContent('SUPERSECRET')
  })
})
