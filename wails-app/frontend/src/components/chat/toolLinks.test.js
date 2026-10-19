import { describe, it, expect } from 'vitest'
import { monoagentCommand, toolLinkOf, TOOL_LINK_PAGES } from './toolLinks.js'

describe('monoagentCommand: each command family opens its tab', () => {
  it.each([
    ['monoagentcli task add "Fix the login bug"', 'task', 'tasks', 'add', 'Fix the login bug'],
    ['monoagentcli tasks list --json', 'task', 'tasks', 'list', ''],
    ['monoagentcli people get 42', 'people', 'people', 'get', '42'],
    ['monoagentcli person list --limit 5', 'people', 'people', 'list', ''],
    ['monoagentcli workflow run wf-123 --input data.json', 'workflow', 'noderunner', 'run', 'wf-123'],
    ['monoagentcli profile documents add ./cv.pdf', 'documents', 'documents', 'add', './cv.pdf'],
    ['monoagentcli secret list', 'vault', 'secretsVault', 'list', ''],
    ['monoagentcli org run growth', 'org', 'orgs', 'run', 'growth'],
  ])('%s', (command, family, page, sub, subject) => {
    expect(monoagentCommand(command)).toEqual({ family, page, sub, subject })
  })

  it('skips the root flags and their values, and finds the CLI by its path', () => {
    expect(monoagentCommand('/home/u/.local/bin/monoagentcli --profile work --json --db-path /tmp/x.db task show 7'))
      .toEqual({ family: 'task', page: 'tasks', sub: 'show', subject: '7' })
  })

  it('takes a flag value for the value, not for the subject', () => {
    expect(monoagentCommand('monoagentcli task add --column ready "Write tests"').subject).toBe('Write tests')
    expect(monoagentCommand('monoagentcli task add --ready "Write tests"').subject).toBe('Write tests')
    expect(monoagentCommand("monoagentcli task add --title='Write tests' x").subject).toBe('x')
  })

  it('names a vault entry, never a value given on the command line', () => {
    expect(monoagentCommand('monoagentcli secret add github --field password=hunter2')).toMatchObject({ family: 'vault', sub: 'add', subject: 'github' })
    expect(monoagentCommand('monoagentcli secret add --value hunter2 github')).toMatchObject({ sub: 'add', subject: 'github' })
    expect(monoagentCommand('echo hunter2 | monoagentcli secret add github --stdin')).toMatchObject({ sub: 'add', subject: 'github' })
  })

  it('finds the call in a chained command line', () => {
    expect(monoagentCommand('cd ~/app && monoagentcli people list --json | jq .')).toMatchObject({ family: 'people', sub: 'list' })
    expect(monoagentCommand('npm test; monoagentcli org status "big org"')).toMatchObject({ family: 'org', subject: 'big org' })
  })

  it('is nothing for other commands, other CLIs or commands that open no tab', () => {
    expect(monoagentCommand('ls -la')).toBeNull()
    expect(monoagentCommand('monomind task list')).toBeNull()
    expect(monoagentCommand('monoagentcli coder status')).toBeNull()
    expect(monoagentCommand('monoagentcli profile list')).toBeNull()
    expect(monoagentCommand('monoagentcli --json')).toBeNull()
    expect(monoagentCommand(undefined)).toBeNull()
  })

  it('covers the six tabs', () => {
    expect(Object.values(TOOL_LINK_PAGES).sort()).toEqual(['documents', 'noderunner', 'orgs', 'people', 'secretsVault', 'tasks'])
  })
})

describe('toolLinkOf', () => {
  it("reads a call's command from its arguments, as an object, a JSON string or an argv array", () => {
    expect(toolLinkOf({ arguments: { command: 'monoagentcli task list' } })).toEqual({ page: 'tasks' })
    expect(toolLinkOf({ arguments: JSON.stringify({ command: 'monoagentcli org run growth' }) })).toEqual({ page: 'orgs' })
    expect(toolLinkOf({ arguments: { command: ['monoagentcli', 'people', 'list'] } })).toEqual({ page: 'people' })
    expect(toolLinkOf({ arguments: { file_path: '/w/a.go' } })).toBeNull()
    expect(toolLinkOf({ arguments: 'not json' })).toBeNull()
  })
})
