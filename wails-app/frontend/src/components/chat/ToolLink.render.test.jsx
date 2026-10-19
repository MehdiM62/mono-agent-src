// @vitest-environment jsdom
// Tool cards that link to the app's tabs (#473): a monoagentcli call on the
// task board, people, workflows, documents, the vault or orgs reads as that
// tab and its card's "Open in <tab>" calls onNavigate with the tab's page id.
import { describe, it, expect, vi, afterEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, screen, fireEvent, cleanup, within } from '@testing-library/react'
import '../../i18n.js'

vi.mock('../../services/hilApi.js', () => ({ listAsks: vi.fn(async () => []), resolve: vi.fn() }))

import { ChatTimeline } from './ChatTimeline.jsx'
import { chatReducer, initialChatState } from './chatReducer.js'
import { StageDrawer } from '../stage/StageDrawer.jsx'
import { OrgChatView } from '../bubbles/OrgChatView.jsx'
import { CoderChatView } from '../bubbles/CoderChatView.jsx'

afterEach(cleanup)

let seq = 0
const ev = (type, payload) => ({ type: 'event', event: { conversationId: 'c1', turnId: 't1', seq: ++seq, at: '2026-10-19T10:00:00.000Z', type, payload } })
const shellCall = (id, command, result = 'ok') => ({
  callId: id, name: 'Bash', native: true, kind: 'shell', arguments: { command }, status: 'completed', ok: true, result,
})
function turn(...calls) {
  return calls.flatMap(c => [
    ev('tool.started', { callId: c.callId, name: c.name, native: true, kind: 'shell', arguments: c.arguments }),
    ev('tool.completed', { callId: c.callId, ok: c.ok, result: c.result }),
  ]).reduce(chatReducer, initialChatState())
}

describe('tool cards link to their tabs', () => {
  it.each([
    ['monoagentcli task add "Fix the login bug"', 'Task board · add “Fix the login bug”', 'Task board', 'tasks'],
    ['monoagentcli people get 42', 'People · get “42”', 'People', 'people'],
    ['monoagentcli workflow run wf-123', 'Workflows · run “wf-123”', 'Workflows', 'noderunner'],
    ['monoagentcli profile documents list', 'Documents · list', 'Documents', 'documents'],
    ['monoagentcli org run growth', 'Orgs · run “growth”', 'Orgs', 'orgs'],
  ])('%s', (command, title, tab, page) => {
    const onNavigate = vi.fn()
    render(<ChatTimeline state={turn(shellCall('c1', command))} turnId="t1" isLive={false} onNavigate={onNavigate} />)
    const card = screen.getByTestId('native-tool-card')
    expect(card).toHaveTextContent(title)
    fireEvent.click(within(card).getByRole('button', { name: `Open in ${tab}` }))
    expect(onNavigate).toHaveBeenCalledWith(page)
  })

  it('shows a vault call by its entry name only: no command line, no output', () => {
    const onNavigate = vi.fn()
    const state = turn(shellCall('v1', 'monoagentcli secret reveal github --reveal --field password', 'password: hunter2'),
      shellCall('v2', 'monoagentcli secret add bank --field pin=4321', 'saved'))
    render(<ChatTimeline state={state} turnId="t1" isLive={false} onNavigate={onNavigate} />)
    const [reveal, add] = screen.getAllByTestId('native-tool-card')
    expect(reveal).toHaveTextContent('Vault · reveal “github”')
    expect(add).toHaveTextContent('Vault · add')
    expect(add).not.toHaveTextContent('bank')
    // Open the cards: still no value, no command line.
    for (const card of [reveal, add]) fireEvent.click(within(card).getByRole('button', { expanded: false }))
    expect(document.body).not.toHaveTextContent('hunter2')
    expect(document.body).not.toHaveTextContent('4321')
    expect(document.body).not.toHaveTextContent('--field')
    expect(screen.getAllByTestId('vault-hidden')).toHaveLength(2)
    fireEvent.click(within(reveal).getByRole('button', { name: 'Open in Vault' }))
    expect(onNavigate).toHaveBeenCalledWith('secretsVault')
  })

  it("shows a vault list's output, names and metadata only", () => {
    render(<ChatTimeline state={turn(shellCall('v3', 'monoagentcli secret list', 'github  login\nbank  card'))} turnId="t1" isLive={false} onNavigate={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { expanded: false }))
    expect(screen.getByTestId('native-tool-card')).toHaveTextContent('github login bank card')
    expect(screen.queryByTestId('vault-hidden')).toBeNull()
  })

  it('shows the command for the other tabs, and no link without onNavigate or for other commands', () => {
    const { unmount } = render(<ChatTimeline state={turn(shellCall('c1', 'monoagentcli task list --json'))} turnId="t1" isLive={false} />)
    expect(screen.getByTestId('native-tool-card')).toHaveTextContent('Task board · list')
    expect(screen.queryByTestId('tool-link')).toBeNull()
    fireEvent.click(screen.getByRole('button', { expanded: false }))
    expect(screen.getByText('monoagentcli task list --json')).toBeInTheDocument()
    unmount()
    render(<ChatTimeline state={turn(shellCall('c2', 'go test ./...'))} turnId="t1" isLive={false} onNavigate={vi.fn()} />)
    expect(screen.getByTestId('native-tool-card')).toHaveTextContent('$ go test ./...')
    expect(screen.queryByTestId('tool-link')).toBeNull()
  })

  it("links from a coder chat's tool cards", () => {
    const onNavigate = vi.fn()
    const conv = {
      messages: [{ role: 'user', content: 'add a task' }, { role: 'turn', turnId: 't1', state: turn(shellCall('c1', 'monoagentcli task add "Ship it"')) }],
      liveTurn: initialChatState(), activeTurnId: '', streaming: false, stopRequested: false, loading: false, loadError: '',
      conversationId: 'c1', send: vi.fn(), stop: vi.fn(),
    }
    render(<CoderChatView conv={conv} isDraft={false} setup={{}} onSetupChange={vi.fn()} draft="" onDraftChange={vi.fn()} onNavigate={onNavigate} />)
    fireEvent.click(screen.getByRole('button', { name: 'Open in Task board' }))
    expect(onNavigate).toHaveBeenCalledWith('tasks')
  })

  it("links from an agent's tool cards in the stage drawer", () => {
    const onNavigate = vi.fn()
    const call = shellCall('w1', 'monoagentcli people list --limit 5')
    const node = { id: 'w1', role: 'researcher', status: 'done', callOrder: ['w1'], parts: [{ kind: 'tool', callId: 'w1' }], messages: [], skills: [], files: [] }
    render(<StageDrawer node={node} calls={{ w1: call }} turnId="t1" isLive={false} onClose={vi.fn()} onNavigate={onNavigate} />)
    fireEvent.click(screen.getByRole('button', { name: 'Open in People' }))
    expect(onNavigate).toHaveBeenCalledWith('people')
  })

  it("links from a role's tool rows in an org bubble, and names a vault call there by its entry only", () => {
    const onNavigate = vi.fn()
    const calls = {
      a: { callId: 'a', name: 'Bash', arguments: { command: 'monoagentcli org status growth' }, status: 'completed', ok: true, startedAt: '2026-10-19T10:00:00Z' },
      b: { callId: 'b', name: 'Bash', arguments: JSON.stringify({ command: 'monoagentcli secret add bank --value 4321' }), status: 'completed', ok: true, startedAt: '2026-10-19T10:00:01Z' },
    }
    const conv = {
      history: { items: [] }, historyError: '', items: [], echoes: [], running: true, sending: false, sendError: '', busy: {}, outcome: {},
      bubble: { calls, callIds: { dev: ['a', 'b'] } }, send: vi.fn(), resolveItem: vi.fn(),
    }
    render(<OrgChatView conv={conv} nameOf={id => id} draft="" onDraftChange={vi.fn()} orgName="growth" view={{ mode: 'role', role: 'dev' }} onView={vi.fn()} onNavigate={onNavigate} />)
    const rows = screen.getAllByTestId('org-chat-tool')
    expect(rows[1]).toHaveTextContent('Vault · add')
    expect(document.body).not.toHaveTextContent('4321')
    fireEvent.click(within(rows[0]).getByRole('button', { name: 'Open in Orgs' }))
    expect(onNavigate).toHaveBeenCalledWith('orgs')
  })
})
