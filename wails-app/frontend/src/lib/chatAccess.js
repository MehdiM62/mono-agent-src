// Access levels of a chat (internal/ai/chat_access.go), weakest first, and the
// pure rules the chat pane applies to them. The CLI is the authority: this only
// decides what to show and which action needs a confirmation.

export const ACCESS_LEVELS = ['scoped', 'coding', 'workspace', 'full']

export const isAccess = a => ACCESS_LEVELS.includes(a)
export const accessRank = a => ACCESS_LEVELS.indexOf(a)

// A level above scoped is a coding-agent session in a folder.
export const isCoderAccess = a => isAccess(a) && a !== 'scoped'

// isRaise: moving from `from` to `to` gives the chat more power. An unknown
// current level counts as a raise, so it always asks.
export function isRaise(from, to) {
  if (!isAccess(from)) return true
  return accessRank(to) > accessRank(from)
}

// accessOfConversation is a conversation record's (or history row's) level; a
// record from before levels takes it from its mode, as the CLI does (a coder
// conversation always had full access).
export function accessOfConversation(c) {
  if (isAccess(c?.access)) return c.access
  return c?.mode === 'coder' ? 'full' : c?.mode ? 'scoped' : ''
}

// The browser permission kind that governs a conversation: the sandboxed
// assistant is the "chat" kind, every coding level the "coder" kind.
export const browserKindFor = access => (isCoderAccess(access) ? 'coder' : 'chat')

// An application-apply task runs confined to the browser: its access cannot be
// changed from the pane and it never spawns workers.
export const CONFINED_KINDS = ['application-apply']
export const isConfinedTask = task => !!task && CONFINED_KINDS.includes(task.kind)

// defaultAccessFor is what a new chat starts at: the operator's coder default
// when coder mode is on and a runtime is ready, else scoped.
export function defaultAccessFor(status, ready) {
  if (status?.enabled && ready && isAccess(status.defaultAccess)) return status.defaultAccess
  return 'scoped'
}

// chosenAccess is the level a draft will be created at: its pick, held to
// scoped while coder mode cannot run.
export function chosenAccess(picked, status, ready) {
  const coderOk = !!status?.enabled && !!ready
  if (isAccess(picked)) return isCoderAccess(picked) && !coderOk ? 'scoped' : picked
  return defaultAccessFor(status, ready)
}
