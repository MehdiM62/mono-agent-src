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

// segments splits a command line into the commands it runs: at &&, ||, ;, |
// and newlines outside quotes, and around each $( … ) and ` … `, whose
// commands run even inside double quotes. Inside single quotes all is text.
function segments(command) {
  const out = []
  let cur = ''
  let quote = ''
  const outer = [] // the quote and closer around each open $( or `
  const cut = () => { out.push(cur); cur = '' }
  for (let i = 0; i < command.length; i++) {
    const c = command[i]
    if (quote === "'") {
      if (c === "'") quote = ''
      cur += c
    } else if (c === '\\' && i + 1 < command.length) {
      cur += c + command[++i]
    } else if (c === '$' && command[i + 1] === '(') {
      outer.push({ quote, close: ')' })
      quote = ''
      i++
      cut()
    } else if (c === '`' && !quote && outer.at(-1)?.close === '`') {
      quote = outer.pop().quote
      cut()
    } else if (c === '`') {
      outer.push({ quote, close: '`' })
      quote = ''
      cut()
    } else if (c === ')' && !quote && outer.at(-1)?.close === ')') {
      quote = outer.pop().quote
      cut()
    } else if (quote) {
      if (c === quote) quote = ''
      cur += c
    } else if (c === '"' || c === "'") {
      quote = c
      cur += c
    } else if (c === ';' || c === '\n' || c === '|' || c === '&') {
      if (c === '&' && command[i + 1] !== '&') { cur += c; continue }
      cut()
      if ((c === '&' || c === '|') && command[i + 1] === c) i++
    } else {
      cur += c
    }
  }
  out.push(cur)
  return out
}

// isSingleCommand says whether a command line runs one command: no chain,
// pipe or command substitution, so its output is that command's alone.
export function isSingleCommand(command) {
  return typeof command === 'string' && segments(command).filter(s => s.trim()).length === 1
}

// A vault command can carry a value (--value, --field k=v, a heredoc), so a
// card that might show one shows nothing of it. vaultRelated is the safe
// rule for text a card cannot read as one monoagentcli call: it names
// monoagentcli anywhere (behind bash -c, sh -c, env, sudo, xargs, eval or a
// variable set to it) and mentions secret, vault, --value or --field
// anywhere. Quotes and backslashes are ignored, so monoagent'cli' counts.
// Better a false hide than a leak (#872 review).
export function vaultRelated(text) {
  if (typeof text !== 'string') return false
  const plain = text.replace(/["'\\]/g, '')
  return plain.includes('monoagentcli') && /secret|vault|--value|--field/i.test(plain)
}

// vaultRelatedCall is vaultRelated for a tool call's arguments read whole
// (an object as its JSON), so a command in any field or cut short counts.
export function vaultRelatedCall(call) {
  const args = call?.arguments
  return vaultRelated(typeof args === 'string' ? args : JSON.stringify(args ?? ''))
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

// monoagentCommand reads the `monoagentcli` runs in a shell command and
// returns the first that opens a tab as {family, page, sub, subject}, or
// null when none does. A vault run anywhere in the line wins over the
// others, so the card is a vault card. sub is the subcommand ("add",
// "list"…), subject the first word after it (a task's title, a person, a
// workflow, an org, a vault entry's name; "" when there is none, and never a
// vault command's file path).
export function monoagentCommand(command) {
  if (typeof command !== 'string' || !command.includes('monoagentcli')) return null
  const runs = segments(command).map(oneRun).filter(Boolean)
  return runs.find(r => r.family === 'vault') || runs[0] || null
}

// The vault subcommands whose argument is a file, not an entry.
const VAULT_FILE_SUBS = new Set(['import', 'export'])

// oneRun reads one segment's monoagentcli run, or null.
function oneRun(seg) {
  const ws = words(seg)
  const at = ws.findIndex(w => w.split('/').pop() === 'monoagentcli')
  if (at < 0) return null
  // Skip the root flags (and the values they take) before the command.
  let i = at + 1
  while (i < ws.length && ws[i].startsWith('-')) {
    if (ROOT_VALUE_FLAGS.has(ws[i])) i++
    i++
  }
  const name = ws[i]
  let family = Object.hasOwn(FAMILY_OF, name) ? FAMILY_OF[name] : undefined
  let rest = i + 1
  if (name === 'profile' && positionals(ws, i + 1)[0] === 'documents') {
    family = 'documents'
    rest = ws.indexOf('documents', i + 1) + 1
  }
  if (!family) return null
  const [sub = '', word = ''] = positionals(ws, rest)
  const path = family === 'vault' && (VAULT_FILE_SUBS.has(sub) || /[\\/~]|^\./.test(word))
  return { family, page: TOOL_LINK_PAGES[family], sub, subject: path ? '' : word }
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
