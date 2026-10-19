// Tool cards that link to the app's tabs (#473): a shell call that runs
// `monoagentcli` on the task board, people, workflows, profile documents,
// the vault or orgs reads as that tab, with the command's subject, and its
// card offers "Open in <tab>". Pure: a command string in, its link out.

// The page id (App.jsx) each command family opens.
export const TOOL_LINK_PAGES = {
  task: 'tasks',
  people: 'people',
  workflow: 'noderunner',
  documents: 'documents',
  vault: 'secretsVault',
  org: 'orgs',
}

// The CLI's command names (and aliases) for each family; `profile
// documents` and the vault's `secret` are special-cased below.
const FAMILY_OF = { task: 'task', tasks: 'task', people: 'people', person: 'people', workflow: 'workflow', secret: 'vault', org: 'org' }

// The root flags that take a value (`monoagentcli --help`).
const ROOT_VALUE_FLAGS = new Set(['--config-dir', '--db-path', '--lang', '--log-file', '--log-level', '--output-dir', '--profile', '--workers'])
// Common subcommand flags that take no value, so the word after one can be
// the subject.
const BOOL_FLAGS = new Set(['--json', '--ready', '--yes', '--all', '--force', '--dry-run', '--reveal', '--headless', '--verbose', '-v'])

// words splits one shell segment into words as a shell would: quotes
// (anywhere in a word) group and are removed, a backslash escapes.
function words(segment) {
  const out = []
  let cur = null
  let quote = ''
  for (let i = 0; i < segment.length; i++) {
    const c = segment[i]
    if (quote) {
      if (c === quote) quote = ''
      else if (c === '\\' && quote === '"' && i + 1 < segment.length) cur += segment[++i]
      else cur += c
    } else if (c === '"' || c === "'") {
      quote = c
      cur = cur ?? ''
    } else if (c === '\\' && i + 1 < segment.length) {
      cur = (cur ?? '') + segment[++i]
    } else if (/\s/.test(c)) {
      if (cur != null) out.push(cur)
      cur = null
    } else {
      cur = (cur ?? '') + c
    }
  }
  if (cur != null) out.push(cur)
  return out
}

// segments splits a command line at &&, ||, ;, | and newlines (outside quotes).
function segments(command) {
  const out = []
  let cur = ''
  let quote = ''
  for (let i = 0; i < command.length; i++) {
    const c = command[i]
    if (quote) {
      if (c === quote && command[i - 1] !== '\\') quote = ''
      cur += c
    } else if (c === '"' || c === "'") {
      quote = c
      cur += c
    } else if (c === ';' || c === '\n' || c === '|' || c === '&') {
      if (c === '&' && command[i + 1] !== '&') { cur += c; continue }
      out.push(cur)
      cur = ''
      if ((c === '&' || c === '|') && command[i + 1] === c) i++
    } else {
      cur += c
    }
  }
  out.push(cur)
  return out
}

// positionals returns the words after start that are not flags or a flag's
// value (a flag written without "=" and not known to take no value is taken
// to have one).
function positionals(ws, start) {
  const out = []
  for (let i = start; i < ws.length; i++) {
    const w = ws[i]
    if (w.startsWith('-')) {
      if (!w.includes('=') && !BOOL_FLAGS.has(w) && i + 1 < ws.length && !ws[i + 1].startsWith('-')) i++
      continue
    }
    out.push(w)
  }
  return out
}

// monoagentCommand finds the first `monoagentcli` run in a shell command and
// returns {family, page, sub, subject}, or null when there is none or its
// command opens no tab. sub is the subcommand ("add", "list"…), subject the
// first word after it (a task's title, a person, a workflow, an org, a
// vault entry's name; "" when there is none).
export function monoagentCommand(command) {
  if (typeof command !== 'string' || !command.includes('monoagentcli')) return null
  for (const seg of segments(command)) {
    const ws = words(seg)
    const at = ws.findIndex(w => w.split('/').pop() === 'monoagentcli')
    if (at < 0) continue
    // Skip the root flags (and the values they take) before the command.
    let i = at + 1
    while (i < ws.length && ws[i].startsWith('-')) {
      if (ROOT_VALUE_FLAGS.has(ws[i])) i++
      i++
    }
    const name = ws[i]
    let family = FAMILY_OF[name]
    let rest = i + 1
    if (name === 'profile' && positionals(ws, i + 1)[0] === 'documents') {
      family = 'documents'
      rest = ws.indexOf('documents', i + 1) + 1
    }
    if (!family) return null
    const [sub = '', subject = ''] = positionals(ws, rest)
    return { family, page: TOOL_LINK_PAGES[family], sub, subject }
  }
  return null
}

// cliTitle is how a monoagentcli call reads: its tab, then the subcommand
// and subject ("Task board · add “Fix the login bug”"). t translates.
export function cliTitle(cli, t) {
  const tab = t(`toolLinks.page.${cli.page}`)
  const what = [cli.sub, cli.subject && `“${cli.subject}”`].filter(Boolean).join(' ')
  return what ? `${tab} · ${what}` : tab
}

// cliOf is monoagentCommand for a tool call: the shell command in its
// arguments (an object, a JSON string or an argv array), or null.
export function cliOf(call) {
  let args = call?.arguments
  if (typeof args === 'string') {
    try { args = JSON.parse(args) } catch { return null }
  }
  const command = Array.isArray(args?.command) ? args.command.join(' ') : args?.command
  return monoagentCommand(command)
}

// toolLinkOf is a tool call's tab link, {page}, or null.
export function toolLinkOf(call) {
  const cli = cliOf(call)
  return cli ? { page: cli.page } : null
}
