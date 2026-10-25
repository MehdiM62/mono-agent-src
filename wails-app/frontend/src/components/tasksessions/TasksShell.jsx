import { useEffect, useState } from 'react'
import { useTaskSessions } from '../../lib/useTaskSessions.js'
import { selectionAfter } from '../../lib/taskSessionModel.js'
import { useSendToCoder } from '../bubbles/CoderBubbleContext.jsx'
import SessionSwitcher from './SessionSwitcher.jsx'
import SessionHeader from './SessionHeader.jsx'
import PlanView from './PlanView.jsx'

// The Tasks tab with its sessions: the switcher on top, and under it one
// board page (Board, the page of Tasks.jsx) for Personal or for the chosen
// session. The page is keyed by its session, so switching remounts it and no
// card of one board is ever shown on another. Personal passes nothing extra:
// it is the board the tab always was.
export default function TasksShell({ isActive = true, Board }) {
  const [selectedId, setSelectedId] = useState(null)
  const [view, setView] = useState('board')
  const s = useTaskSessions(isActive, selectedId)
  const { openDraft } = useSendToCoder() || {}
  // A session that is archived or gone leaves the list: fall back to Personal.
  useEffect(() => {
    if (selectedId && s.sessions.length && selectionAfter(selectedId, s.sessions) === null) setSelectedId(null)
  }, [selectedId, s.sessions])
  const choose = (id) => { setSelectedId(id); setView('board') }
  const session = s.selected

  const switcher = <SessionSwitcher sessions={s.sessions} selectedId={selectedId} onSelect={choose} />
  if (!selectedId || !session) {
    return <Board key="personal" isActive={isActive} switcher={switcher} />
  }
  const header = (
    <SessionHeader
      session={session} view={view} onView={setView} onAct={s.act} onBudget={s.setBudget}
      notice={s.notice} onClearNotice={s.clearNotice}
      onOpenConversation={openDraft ? (id) => openDraft({ conversationId: id }) : null}
    />
  )
  return (
    <Board
      key={`session-${selectedId}`} isActive={isActive} switcher={switcher}
      session={session} sessionEvents={s.events} sessionHeader={header}
      renderPlan={view === 'plan' ? ({ openTask }) => <PlanView sessionId={selectedId} rev={session.rev} onOpen={openTask} /> : null}
    />
  )
}
