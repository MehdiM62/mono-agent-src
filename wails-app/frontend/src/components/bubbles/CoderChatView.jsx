import { Fragment, useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ArrowDown, Loader } from 'lucide-react'
import { api, notify } from '../../services/api.js'
import { ChatTimeline } from '../chat/ChatTimeline.jsx'
import { TurnStatus } from '../chat/TurnStatus.jsx'
import { ChatComposer } from '../chat/ChatComposer.jsx'
import { MessageAttachments, WorkspaceImageContext } from '../chat/ChatAttachments.jsx'
import { takeFiles, MAX_ATTACHMENTS } from '../chat/chatAttachments.js'
import { useChatScroll } from '../chat/useChatScroll.js'
import { CoderInitNote } from '../chat/CoderHeader.jsx'
import { ChatSetup, BlockedReason } from './ChatSetup.jsx'
import { useChatSetup } from './useChatSetup.js'
import { isCoderAccess } from '../../lib/chatAccess.js'
import { MessageBubble } from '../chat/messageBubble.jsx'
import { AskCard } from './AskCard.jsx'
import { useHilAsks } from './useHilAsks.js'
import { isAgentNotSetup } from '../../lib/agentSetup.js'
import AgentSetupLink from '../AgentSetupLink.jsx'
import '../chat/chat.css'

const mono = 'var(--font-mono)'
const srOnly = { position: 'absolute', width: 1, height: 1, padding: 0, margin: -1, overflow: 'hidden', clip: 'rect(0,0,0,0)', whiteSpace: 'nowrap', border: 0 }

// CoderChatView is the chat half of an expanded coder bubble: the
// transcript, the live turn and the composer. conv is useCoderConversation's
// result; draft/onDraftChange keep the unsent text across collapses.
export function CoderChatView({ conv, isDraft, access, setup, onSetupChange, draft, onDraftChange, initialScrollTop, onScroll, onNavigate, agentFilter = null, agentFilterTurnId = '', agentFilterName = '', onClearAgentFilter }) {
  const { t } = useTranslation()
  const setupState = useChatSetup(isDraft, setup, onSetupChange)
  // The level this chat runs at: the draft's resolved pick, then the bubble's.
  const level = isDraft ? setupState.access : access
  // Sends and workflow approvals this chat's agents prepared wait here, in
  // the same card the org bubble uses.
  const hil = useHilAsks({ conversationId: conv.conversationId || '' }, !!conv.conversationId)
  const scroll = useChatScroll(`${conv.messages.length}:${conv.liveTurn.lastSeq}:${hil.asks.length}`)

  // Put the transcript back where it was when the bubble collapsed.
  useEffect(() => {
    const el = scroll.containerRef.current
    if (el && initialScrollTop != null) el.scrollTop = initialScrollTop
    // Only on mount.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Files pasted, dropped or picked in the composer, sent with the next
  // message (CLI `chat --attach`).
  const [attachments, setAttachments] = useState([])
  const attach = useCallback(async (files) => {
    const { added, skipped } = await takeFiles(attachments.length, files)
    if (added.length) setAttachments(list => [...list, ...added].slice(0, MAX_ATTACHMENTS))
    if (skipped.length) notify('attach', `Not attached: ${skipped.join('; ')}`)
  }, [attachments.length])
  const removeAttachment = useCallback(id => setAttachments(list => list.filter(a => a.id !== id)), [])

  const send = useCallback(async () => {
    const text = draft
    if (!text.trim()) return
    const sent = attachments
    onDraftChange('')
    setAttachments([])
    const ok = await conv.send(text, sent)
    if (ok === false && !conv.conversationId) {
      onDraftChange(text)
      setAttachments(sent)
    }
  }, [draft, attachments, conv, onDraftChange])

  const blocked = isDraft && setupState.state !== 'ready'
  const reason = blocked ? <BlockedReason setup={setupState} onNavigate={onNavigate} compact /> : ''

  // What a screen reader hears: a finished turn, a notice, and the composer's
  // reason changing (the banner changes as the runtime scan settles).
  const [reasonSpoken, setReasonSpoken] = useState('')
  const lastState = useRef(setupState.state)
  useEffect(() => {
    if (!isDraft || setupState.state === lastState.current) return
    lastState.current = setupState.state
    setReasonSpoken(setupState.state === 'ready' ? t('bubbles.pane.canSend') : t(`bubbles.pane.state.${setupState.state}`))
  }, [isDraft, setupState.state, t])
  const announcement = conv.announcement || reasonSpoken
  const empty = conv.messages.length === 0 && !conv.streaming

  return (
    <div style={{ display: 'flex', flexDirection: 'column', minHeight: 0, flex: 1 }}>
      {agentFilter && (
        <div className="stage-filter" data-testid="chat-agent-filter">
          <span>{t('stage.filterShowing', { name: agentFilterName })}</span>
          <span style={{ flex: 1 }} />
          <button type="button" className="btn btn-ghost btn-sm" onClick={onClearAgentFilter} style={{ fontSize: 10 }}>{t('stage.filterClear')}</button>
        </div>
      )}
      <div role="status" aria-live="polite" aria-label={t('bubbles.pane.announcements')} style={srOnly}>{announcement}</div>
      {isDraft && empty && <ChatSetup s={setupState} setup={setup} onChange={onSetupChange} onNavigate={onNavigate} />}
      <div style={{ flex: 1, position: 'relative', minHeight: 0 }}>
        <div ref={scroll.containerRef} onScroll={e => onScroll?.(e.currentTarget.scrollTop)} data-testid="bubble-transcript"
          style={{ height: '100%', overflowY: 'auto', padding: 12, display: 'flex', flexDirection: 'column' }}>
          {conv.loading && (
            <div style={{ display: 'flex', alignItems: 'center', gap: 6, fontFamily: mono, fontSize: 10, color: 'var(--text-muted)' }}>
              <Loader size={11} className="chat-spin" /> {t('bubbles.loadingChat')}
            </div>
          )}
          {conv.loadError && <MessageBubble role="error" isError content={conv.loadError} />}
          {empty && !conv.loading && !isDraft && (
            <div style={{ margin: 'auto', fontFamily: mono, fontSize: 11, color: 'var(--text-muted)' }}>{t('bubbles.emptyChat')}</div>
          )}
          <WorkspaceImageContext.Provider value={conv.conversationId || ''}>
            {conv.messages.map((msg, i) => (
              msg.role === 'coder-init' ? <CoderInitNote key={i} workspace={msg.workspace} />
                : msg.role === 'turn' ? (
                  <div key={i} className="chat-assistant-turn">
                    <ChatTimeline state={msg.state} turnId={msg.turnId} isLive={false} agentFilter={msg.turnId === agentFilterTurnId ? agentFilter : null} onNavigate={onNavigate} />
                    <TurnStatus state={msg.state} stopRequested={false} ownedByThisInstance={msg.ownedByThisInstance} />
                    {isAgentNotSetup(msg.state.terminal?.code) && <AgentSetupLink onNavigate={onNavigate} />}
                  </div>
                ) : (
                  <Fragment key={i}>
                    <MessageBubble role={msg.role} content={msg.content} isError={msg.role === 'error'} code={msg.code} onNavigate={onNavigate} />
                    <MessageAttachments attachments={msg.attachments} conversationId={conv.conversationId} />
                  </Fragment>
                )
            ))}
            {conv.streaming && (
              <div className="chat-assistant-turn">
                <ChatTimeline state={conv.liveTurn} turnId={conv.activeTurnId} isLive agentFilter={conv.activeTurnId === agentFilterTurnId ? agentFilter : null} onNavigate={onNavigate} />
                <TurnStatus state={conv.liveTurn} stopRequested={conv.stopRequested} />
              </div>
            )}
          </WorkspaceImageContext.Provider>
          {hil.asks.length > 0 && (
            <div data-testid="coder-asks" role="region" aria-label={t('ask.regionLabel')} aria-live="polite">
              {hil.asks.map(ask => (
                <AskCard key={ask.id} ask={ask} busy={!!hil.busy[ask.id]} outcome={hil.outcome[ask.id]}
                  onDecide={hil.resolve} onNavigate={onNavigate} />
              ))}
            </div>
          )}
        </div>
        {!scroll.isFollowing && scroll.unreadCount > 0 && (
          <button onClick={scroll.jumpToLatest} className="bubble-jump">
            <ArrowDown size={11} /> {t('bubbles.jumpToLatest')}
          </button>
        )}
      </div>
      <ChatComposer value={draft} onChange={onDraftChange} onSend={send} onStop={conv.stop}
        streaming={conv.streaming} disabled={blocked || conv.loading}
        disabledReason={reason}
        attachments={attachments} onAttach={isCoderAccess(level) ? attach : undefined} onRemoveAttachment={removeAttachment} />
    </div>
  )
}
