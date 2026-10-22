import * as GoApp from '../wailsjs/go/main/App'

// The access-level bindings (app_history.go), kept apart from services/api.js.
// Each call starts inside a promise, so a synchronous throw (no Wails bridge)
// lands in the caller's catch. A refusal rejects with the CLI's message; the
// CLI's code, when the message names one, is on .code.
function failure(e) {
  const message = String(e?.message || e || '')
  const err = new Error(message)
  err.code = e?.code || (/operator[_ ]only/i.test(message) ? 'operator_only' : '')
  return err
}
const call = fn => Promise.resolve().then(fn).catch(e => { throw failure(e) })

export const chatAccessApi = {
  // A conversation at an access level: scoped has no folder; any other level
  // works in cwd, or in the profile's folder when cwd is ''. Resolves the
  // Conversation ({id, runtimeId, model, effort, access, cwd, orgMode, …}).
  create: (workflowID, runtimeID, model, effort, access, cwd = '') =>
    call(() => GoApp.CreateChatConversationAccess(workflowID, runtimeID, model, effort, access, cwd)),
  // {conversation, previousAccess, lowered: null | {stopped, survivors:[{pid,
  // command, reason}], workersDropped, taskRevoked}}. Raising runs as the GUI
  // operator; the CLI refuses it from an agent.
  setAccess: (id, level) => call(() => GoApp.SetChatAccess(id, level)),
}
