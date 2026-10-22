import { Bot, Code2 } from 'lucide-react'
import { missingText, coderReady, useActiveProfile } from './useCoderMode.js'
import { FolderPicker } from './FolderPicker.jsx'

// CoderModePicker is the new-chat choice between the Assistant and Coder
// modes (#203), plus Coder's workspace: the active profile's folder
// (default), a folder picked with the native dialog, or a recent one. Shown only before
// a conversation exists, since its mode can't change once it starts. Renders
// nothing while coder mode is off in Settings. runtimeId is the coding
// runtime the chat will run on (picked in the runtime selector); the list
// under the workspaces says which runtimes are ready.
//
// workspace is { kind: 'root' } or { kind: 'folder', path }. workspaceOnly
// hides the mode switch, for a chat that is already a coder chat (a new
// coder bubble, #227).

const mono = 'var(--font-mono)'
const CYAN = '#00b4d8'

function segStyle(active, disabled) {
  return {
    flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 5,
    padding: '5px 8px', borderRadius: 6, font: 'inherit', fontFamily: mono, fontSize: 10, fontWeight: 600, letterSpacing: 0.5,
    cursor: disabled ? 'not-allowed' : 'pointer', opacity: disabled ? 0.45 : 1,
    background: active ? 'rgba(0,180,216,0.15)' : 'transparent',
    border: `1px solid ${active ? 'rgba(0,180,216,0.45)' : 'rgba(0,180,216,0.12)'}`,
    color: active ? CYAN : 'var(--text-muted)',
  }
}

export function CoderModePicker({ status, mode, onModeChange, workspace, onWorkspaceChange, recent = [], onPickFolder, runtimeId = '', disabled = false, workspaceOnly = false }) {
  const profile = useActiveProfile(!!status?.enabled)
  if (!status?.enabled) return null
  const ready = coderReady(status)

  return (
    <div data-testid="coder-mode-picker" style={{ padding: '8px 12px', borderBottom: '1px solid rgba(0,180,216,0.06)', display: 'flex', flexDirection: 'column', gap: 6, flexShrink: 0 }}>
      {!workspaceOnly && <div role="radiogroup" aria-label="Chat mode" style={{ display: 'flex', gap: 6 }}>
        <button type="button" role="radio" aria-checked={mode === 'assistant'} disabled={disabled}
          onClick={() => onModeChange('assistant')} style={segStyle(mode === 'assistant', disabled)}>
          <Bot size={11} /> Assistant
        </button>
        <button type="button" role="radio" aria-checked={mode === 'coder'} disabled={disabled || !ready}
          title={ready ? 'A coding agent with full access inside one folder' : `Coder mode ${missingText(status)}`}
          onClick={() => onModeChange('coder')} style={segStyle(mode === 'coder', disabled || !ready)}>
          <Code2 size={11} /> Coder
        </button>
      </div>}
      {!ready && (
        <div data-testid="coder-not-ready" style={{ fontFamily: mono, fontSize: 9.5, color: '#fbbf24', lineHeight: 1.5 }}>
          Coder mode {missingText(status)}.
        </div>
      )}

      {mode === 'coder' && ready && (
        <FolderPicker status={status} workspace={workspace} onWorkspaceChange={onWorkspaceChange} recent={recent}
          onPickFolder={onPickFolder} runtimeId={runtimeId} disabled={disabled} profile={profile} />
      )}
    </div>
  )
}
