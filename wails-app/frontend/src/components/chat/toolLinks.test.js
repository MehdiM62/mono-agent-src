import { describe, it, expect } from 'vitest'
import { monoagentCommand, toolLinkOf, cliTitle, vaultRelated, vaultRelatedCall, isSingleCommand, TOOL_LINK_PAGES } from './toolLinks.js'

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

  // Only the word right after get, reveal, update or rm is a vault entry's
  // name; any other word may be a value, whatever the flag table says
  // (#872 review: -v is a boolean flag elsewhere, a value flag here).
  it('names a vault entry, never a value given on the command line', () => {
    expect(monoagentCommand('monoagentcli secret get github --json')).toMatchObject({ family: 'vault', sub: 'get', subject: 'github' })
    expect(monoagentCommand('monoagentcli secret reveal github --reveal')).toMatchObject({ sub: 'reveal', subject: 'github' })
    expect(monoagentCommand('monoagentcli secret update github --field password=hunter2')).toMatchObject({ sub: 'update', subject: 'github' })
    for (const command of [
      'monoagentcli secret add -n tok -v hunter2',
      'monoagentcli secret add github --field password=hunter2',
      'monoagentcli secret add --value hunter2 github',
      'echo hunter2 | monoagentcli secret add github --stdin',
      'monoagentcli secret update -v hunter2 github',
      'monoagentcli secret -v hunter2 add',
    ]) {
      const cli = monoagentCommand(command)
      expect(cli.family, command).toBe('vault')
      expect(cli.subject, command).toBe('')
      expect(JSON.stringify(cli), command).not.toContain('hunter2')
    }
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

// #872 review: a vault command anywhere in a command line, or wrapped so it
// can't be parsed, never shows its command line or output.
describe('vault commands, wherever they sit', () => {
  const t = (k) => ({ 'toolLinks.page.secretsVault': 'Vault', 'toolLinks.page.tasks': 'Task board' }[k] || k)

  it('reads a chained command by its vault segment, whichever segment it is', () => {
    expect(monoagentCommand('monoagentcli task list && monoagentcli secret reveal GITHUB_TOKEN --reveal'))
      .toMatchObject({ family: 'vault', sub: 'reveal', subject: 'GITHUB_TOKEN' })
    expect(monoagentCommand('monoagentcli task list; monoagentcli secret add --name t --value SUPERSECRET'))
      .toMatchObject({ family: 'vault', sub: 'add', subject: '' })
    expect(monoagentCommand('monoagentcli task list || monoagentcli secret get t')).toMatchObject({ family: 'vault' })
    expect(monoagentCommand('monoagentcli task list\nmonoagentcli secret get t')).toMatchObject({ family: 'vault' })
  })

  it('looks inside $( ) and backticks, in or out of double quotes', () => {
    expect(monoagentCommand('echo "$(monoagentcli secret get tok)"')).toMatchObject({ family: 'vault', sub: 'get', subject: 'tok' })
    expect(monoagentCommand('X=$(monoagentcli secret get tok) monoagentcli task list')).toMatchObject({ family: 'vault' })
    expect(monoagentCommand('echo `monoagentcli secret get tok`')).toMatchObject({ family: 'vault', subject: 'tok' })
    // Parentheses in a quoted title are text, not a subshell.
    expect(monoagentCommand('monoagentcli task add "Fix (urgent) bug"').subject).toBe('Fix (urgent) bug')
  })

  it('never names a file path: an import or export reads as the command alone', () => {
    const imp = monoagentCommand('monoagentcli secret import /home/user/private/vault-export.json.enc')
    expect(imp).toMatchObject({ family: 'vault', sub: 'import', subject: '' })
    expect(cliTitle(imp, t)).toBe('Vault · import')
    expect(monoagentCommand('monoagentcli secret import vault-export.json.enc').subject).toBe('')
    expect(monoagentCommand('monoagentcli secret export --output /home/user/private/out.enc').subject).toBe('')
    expect(monoagentCommand('monoagentcli secret get ./tok').subject).toBe('')
  })

  it('opens no tab for a command named like an object property', () => {
    for (const name of ['toString', 'constructor', '__proto__', 'hasOwnProperty', 'valueOf']) {
      expect(monoagentCommand(`monoagentcli ${name} foo`)).toBeNull()
    }
  })

  it.each([
    'monoagentcli secret add --name tok --value SUPERSECRET',
    'monoagentcli task list && monoagentcli secret reveal GITHUB_TOKEN --reveal',
    'bash -c "monoagentcli secret add --name t --value SUPERSECRET"',
    "sh -c 'monoagentcli secret reveal t --reveal'",
    'CLI=monoagentcli; $CLI secret add --name t --value SUPERSECRET',
    'env X=1 monoagentcli secret get tok',
    'sudo -u me monoagentcli secret get tok',
    'echo tok | xargs monoagentcli secret get',
    'eval "monoagentcli secret get tok"',
    "monoagent'cli' secret get tok",
    'monoagent\\cli vault list',
    'monoagentcli people update 4 --field email=a@b.c',
  ])('takes %s for vault-related', (command) => {
    expect(vaultRelated(command)).toBe(true)
  })

  it.each([
    'monoagentcli task add "Fix the login bug"',
    'monoagentcli people list --json',
    'echo my secret value',
    'ls -la',
  ])('takes %s for not vault-related', (command) => {
    expect(vaultRelated(command)).toBe(false)
  })

  it('reads a call whole, arguments cut short included', () => {
    expect(vaultRelatedCall({ arguments: '{"command":"monoagentcli secret add --name t --value SUPERSE' })).toBe(true)
    expect(vaultRelatedCall({ arguments: { command: ['bash', '-c', 'monoagentcli secret get t'] } })).toBe(true)
    expect(vaultRelatedCall({ arguments: { command: 'monoagentcli task list' } })).toBe(false)
    expect(vaultRelatedCall({})).toBe(false)
  })

  it('tells one command from a command line', () => {
    expect(isSingleCommand('monoagentcli secret list --json')).toBe(true)
    expect(isSingleCommand('monoagentcli secret list && cat ~/.ssh/id_rsa')).toBe(false)
    expect(isSingleCommand('monoagentcli secret list | grep x')).toBe(false)
    expect(isSingleCommand('echo $(monoagentcli secret list)')).toBe(false)
  })
})

// #872 review, round 2: MonoAgent's commands that take or print a secret
// outside the vault are hidden like vault commands, wrapped or not.
describe("MonoAgent's own secret commands", () => {
  const S = 'Zq8Xfake4417'
  it.each([
    `monoagentcli profile info set bank_iban ${S}`,
    `monoagentcli --profile work profile info set card_number ${S}`,
    'monoagentcli profile info get national_id --reveal',
    'monoagentcli account login --email me@x.test --code 123456',
    `monoagentcli jev key set ${S}`,
    `monoagentcli connect set-oauth-client outlook --client-id abc --client-secret ${S}`,
    'monoagentcli connect get-oauth-client outlook --reveal',
    'monoagentcli api key create --name ci',
    `bash -c "monoagentcli profile info set iban ${S}"`,
    'monoagentcli task list && monoagentcli api key create --name ci',
  ])('%s is hidden', (command) => {
    expect(vaultRelated(command)).toBe(true)
  })

  it.each([
    'monoagentcli profile info get national_id',
    'monoagentcli api key list',
    'monoagentcli account status',
    'monoagentcli jev status',
    'monoagentcli task add "set the profile info"',
  ])('%s is not', (command) => {
    expect(vaultRelated(command)).toBe(false)
  })

  it('reads through invisible characters and width variants', () => {
    expect(vaultRelated(`mono\u200bagentcli secret add --value ${S}`)).toBe(true)
    expect(vaultRelated(`monoagentcli \uff53ecret add --name t`)).toBe(true)
    expect(vaultRelated(`mono\u2060agentcli account login --code 1`)).toBe(true)
  })
})

// #872 review, round 3: more of MonoAgent's commands that take or print a
// secret, and the binary's name in any case.
describe("MonoAgent's own secret commands, round 3", () => {
  const S = 'Zq8Xfake4417'
  it.each([
    `monoagentcli library login --email me@x.test --code ${S}`,
    `printf %s '{"value":"${S}"}' | monoagentcli connect save linear --method apikey --stdin-json`,
    `monoagentcli connect save linear --method apikey --stdin-json <<'EOF'\n{"value":"${S}"}\nEOF`,
    `printf %s '{"consumer_key":"${S}"}' | monoagentcli connect oauth trello --stdin-json`,
    'monoagentcli extension pair',
    `MONOAGENTCLI secret add --value ${S}`,
    `monoagent secret add --value ${S}`,
  ])('%s is hidden', (command) => {
    expect(vaultRelated(command)).toBe(true)
  })

  it.each([
    'monoagentcli connect show github',
    'monoagentcli api key show ci',
    'monoagentcli extension status',
    'monoagentcli connect oauth github',
    'monoagentcli library list',
  ])('%s is not: it prints no secret', (command) => {
    expect(vaultRelated(command)).toBe(false)
  })
})
