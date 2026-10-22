import React from 'react'
import { ListTodo, CheckCircle2, Clock, Circle } from 'lucide-react'
import { sectionCardStyle, sectionTitleStyle, subtleBtnStyle } from './constants.jsx'

function TaskStatusBadge({ status }) {
  const norm = (status || 'todo').toLowerCase()
  let bg = 'rgba(100,116,139,0.15)'
  let color = '#94a3b8'

  if (norm === 'done' || norm === 'completed') {
    bg = 'rgba(16,185,129,0.15)'
    color = '#10b981'
  } else if (norm === 'in_progress' || norm === 'running') {
    bg = 'rgba(0,180,216,0.15)'
    color = '#00b4d8'
  } else if (norm === 'ready') {
    bg = 'rgba(245,158,11,0.15)'
    color = '#f59e0b'
  }

  return (
    <span
      role="status"
      aria-label={`Task status: ${norm}`}
      style={{
        background: bg,
        border: `1px solid ${color}33`,
        borderRadius: 4,
        padding: '1px 6px',
        fontSize: 10,
        fontWeight: 600,
        color,
        fontFamily: 'var(--font-mono)',
        textTransform: 'uppercase',
      }}
    >
      {norm.replace(/_/g, ' ')}
    </span>
  )
}

export default function ApplicationTasksSection({ tasks = [], onNavigate }) {
  const taskList = Array.isArray(tasks) ? tasks : []

  const handleOpenTask = (t) => {
    if (typeof onNavigate === 'function') {
      onNavigate('tasks', { taskId: t.id })
    }
  }

  return (
    <section aria-labelledby="section-tasks-title" style={sectionCardStyle}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
        <h3 id="section-tasks-title" style={sectionTitleStyle}>
          <ListTodo size={14} style={{ color: '#00b4d8' }} />
          Tasks ({taskList.length})
        </h3>
        <span style={{ fontSize: 11, color: 'var(--text-muted, #64748b)', fontFamily: 'var(--font-mono)' }}>
          Board items
        </span>
      </div>

      {taskList.length === 0 ? (
        <div
          style={{
            padding: '12px 14px',
            borderRadius: 6,
            background: 'rgba(0,0,0,0.2)',
            color: 'var(--text-muted, #64748b)',
            fontSize: 12,
            fontFamily: 'var(--font-mono)',
            textAlign: 'center',
          }}
        >
          No tasks linked to this application.
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          {taskList.map((t, idx) => (
            <div
              key={t.id || idx}
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 12,
                padding: '8px 12px',
                borderRadius: 6,
                background: 'rgba(0,0,0,0.25)',
                border: '1px solid rgba(255,255,255,0.05)',
                fontFamily: 'var(--font-mono)',
                fontSize: 11,
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0, flex: 1 }}>
                <Circle size={10} style={{ color: '#00b4d8', flexShrink: 0 }} />
                <span
                  style={{
                    color: 'var(--text-primary, #f1f5f9)',
                    fontWeight: 500,
                    wordBreak: 'break-word',
                  }}
                >
                  {t.title || 'Untitled task'}
                </span>
                <TaskStatusBadge status={t.status} />
              </div>

              {onNavigate && (
                <button
                  type="button"
                  onClick={() => handleOpenTask(t)}
                  style={subtleBtnStyle}
                  aria-label={`Open task board for ${t.title || 'task'}`}
                >
                  View
                </button>
              )}
            </div>
          ))}
        </div>
      )}
    </section>
  )
}
