import React from 'react'
import { History, ArrowRight, User, Bot, Server } from 'lucide-react'
import { sectionCardStyle, sectionTitleStyle } from './constants.jsx'

function ActorIcon({ actor }) {
  const norm = (actor || '').toLowerCase()
  if (norm.includes('agent') || norm.includes('coder') || norm.includes('bot')) {
    return <Bot size={11} style={{ color: '#00b4d8' }} />
  }
  if (norm.includes('user') || norm.includes('human') || norm.includes('operator')) {
    return <User size={11} style={{ color: '#10b981' }} />
  }
  return <Server size={11} style={{ color: '#94a3b8' }} />
}

export default function ApplicationStatusHistorySection({ statusLog = [] }) {
  const log = Array.isArray(statusLog) ? statusLog : []

  return (
    <section aria-labelledby="section-status-history-title" style={sectionCardStyle}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
        <h3 id="section-status-history-title" style={sectionTitleStyle}>
          <History size={14} style={{ color: '#00b4d8' }} />
          Status History ({log.length})
        </h3>
      </div>

      {log.length === 0 ? (
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
          No status history recorded.
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          {log.map((entry, idx) => {
            const from = entry.from_status || '(created)'
            const to = entry.to_status || 'unknown'
            const actor = entry.actor || 'system'
            const time = entry.created_at || entry.createdAt || ''

            return (
              <div
                key={idx}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 12,
                  padding: '7px 10px',
                  borderRadius: 6,
                  background: 'rgba(0,0,0,0.25)',
                  border: '1px solid rgba(255,255,255,0.05)',
                  fontFamily: 'var(--font-mono)',
                  fontSize: 11,
                  flexWrap: 'wrap',
                }}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                  <span style={{ color: 'var(--text-secondary, #94a3b8)' }}>{from}</span>
                  <ArrowRight size={11} style={{ color: '#64748b' }} />
                  <span style={{ color: '#00b4d8', fontWeight: 600 }}>{to}</span>
                  <span
                    style={{
                      display: 'inline-flex',
                      alignItems: 'center',
                      gap: 4,
                      color: 'var(--text-muted, #64748b)',
                      fontSize: 10,
                      background: 'rgba(255,255,255,0.04)',
                      padding: '1px 5px',
                      borderRadius: 3,
                    }}
                  >
                    <ActorIcon actor={actor} />
                    {actor}
                  </span>
                </div>

                {time && (
                  <span style={{ color: 'var(--text-muted, #64748b)', fontSize: 10 }}>
                    {time.slice(0, 19).replace('T', ' ')}
                  </span>
                )}
              </div>
            )
          })}
        </div>
      )}
    </section>
  )
}
