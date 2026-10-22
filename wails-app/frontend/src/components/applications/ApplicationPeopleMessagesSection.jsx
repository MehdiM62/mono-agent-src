import React from 'react'
import { Users, Mail, ArrowDownLeft, ArrowUpRight, MessageSquare, ExternalLink } from 'lucide-react'
import { sectionCardStyle, sectionTitleStyle, subtleBtnStyle } from './constants.jsx'

export default function ApplicationPeopleMessagesSection({ people = [], messages = [], onNavigate }) {
  const peopleList = Array.isArray(people) ? people : []
  const messageList = Array.isArray(messages) ? messages : []

  const hasPeople = peopleList.length > 0
  const hasMessages = messageList.length > 0
  const isEmpty = !hasPeople && !hasMessages

  const handleOpenCommunicationsPerson = (p) => {
    if (typeof onNavigate === 'function') {
      onNavigate('communications', { person: p.id, personName: p.name })
    }
  }

  const handleOpenCommunicationsMessage = (m) => {
    if (typeof onNavigate === 'function') {
      onNavigate('communications', { messageId: m.id, hil_ref: m.id, person: m.personId })
    }
  }

  return (
    <section aria-labelledby="section-people-messages-title" style={sectionCardStyle}>
      <h3 id="section-people-messages-title" style={sectionTitleStyle}>
        <Users size={14} style={{ color: '#00b4d8' }} />
        People & Messages
      </h3>

      {isEmpty ? (
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
          No people or messages linked to this application.
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
          {/* People Subsection */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            <span
              style={{
                fontSize: 10,
                fontWeight: 600,
                textTransform: 'uppercase',
                letterSpacing: '0.04em',
                color: 'var(--text-muted, #64748b)',
                fontFamily: 'var(--font-mono)',
              }}
            >
              Recruiter & Contacts ({peopleList.length})
            </span>

            {!hasPeople ? (
              <div style={{ fontSize: 11, color: 'var(--text-muted, #64748b)', fontFamily: 'var(--font-mono)', padding: '4px 0' }}>
                No recruiter or contacts linked.
              </div>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                {peopleList.map((p, i) => (
                  <div
                    key={p.id || i}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      gap: 10,
                      padding: '7px 10px',
                      borderRadius: 6,
                      background: 'rgba(0,0,0,0.25)',
                      border: '1px solid rgba(255,255,255,0.05)',
                      fontFamily: 'var(--font-mono)',
                      fontSize: 11,
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                      <Users size={13} style={{ color: '#64748b', flexShrink: 0 }} />
                      <div style={{ display: 'flex', flexDirection: 'column', minWidth: 0 }}>
                        <span style={{ fontWeight: 600, color: 'var(--text-primary, #f1f5f9)' }}>
                          {p.name || 'Unnamed Contact'}
                        </span>
                        <div style={{ display: 'flex', gap: 6, color: 'var(--text-muted, #94a3b8)', fontSize: 10 }}>
                          {p.role && <span>{p.role}</span>}
                          {p.role && p.email && <span>·</span>}
                          {p.email && <span>{p.email}</span>}
                        </div>
                      </div>
                    </div>

                    <button
                      type="button"
                      onClick={() => handleOpenCommunicationsPerson(p)}
                      style={subtleBtnStyle}
                      aria-label={`Open communications for ${p.name || 'contact'}`}
                    >
                      <MessageSquare size={11} /> Communications
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* Messages Subsection */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            <span
              style={{
                fontSize: 10,
                fontWeight: 600,
                textTransform: 'uppercase',
                letterSpacing: '0.04em',
                color: 'var(--text-muted, #64748b)',
                fontFamily: 'var(--font-mono)',
              }}
            >
              Messages & Threads ({messageList.length})
            </span>

            {!hasMessages ? (
              <div style={{ fontSize: 11, color: 'var(--text-muted, #64748b)', fontFamily: 'var(--font-mono)', padding: '4px 0' }}>
                No messages or threads linked.
              </div>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                {messageList.map((m, i) => {
                  const isIncoming = m.direction === 'in'
                  return (
                    <div
                      key={m.id || i}
                      style={{
                        display: 'flex',
                        alignItems: 'flex-start',
                        justifyContent: 'space-between',
                        gap: 12,
                        padding: '8px 10px',
                        borderRadius: 6,
                        background: 'rgba(0,0,0,0.25)',
                        border: '1px solid rgba(255,255,255,0.05)',
                        fontFamily: 'var(--font-mono)',
                        fontSize: 11,
                      }}
                    >
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 3, minWidth: 0, flex: 1 }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
                          <span
                            style={{
                              background: isIncoming ? 'rgba(16,185,129,0.15)' : 'rgba(59,130,246,0.15)',
                              color: isIncoming ? '#10b981' : '#60a5fa',
                              border: `1px solid ${isIncoming ? 'rgba(16,185,129,0.3)' : 'rgba(59,130,246,0.3)'}`,
                              borderRadius: 3,
                              padding: '1px 5px',
                              fontSize: 9,
                              fontWeight: 600,
                              textTransform: 'uppercase',
                              display: 'inline-flex',
                              alignItems: 'center',
                              gap: 3,
                            }}
                          >
                            {isIncoming ? <ArrowDownLeft size={10} /> : <ArrowUpRight size={10} />}
                            {isIncoming ? 'Incoming' : 'Outgoing'}
                          </span>

                          {m.channel && (
                            <span style={{ color: 'var(--text-muted, #64748b)', fontSize: 10, textTransform: 'uppercase' }}>
                              [{m.channel}]
                            </span>
                          )}

                          <span style={{ fontWeight: 600, color: 'var(--text-primary, #f1f5f9)' }}>
                            {m.subject || '(no subject)'}
                          </span>
                        </div>

                        {m.snippet && (
                          <div
                            style={{
                              color: 'var(--text-secondary, #94a3b8)',
                              fontSize: 11,
                              lineHeight: 1.4,
                              overflow: 'hidden',
                              textOverflow: 'ellipsis',
                              whiteSpace: 'nowrap',
                              maxWidth: '100%',
                            }}
                          >
                            {m.snippet}
                          </div>
                        )}

                        {m.at && (
                          <div style={{ color: 'var(--text-muted, #64748b)', fontSize: 9 }}>
                            {m.at.slice(0, 16).replace('T', ' ')}
                          </div>
                        )}
                      </div>

                      <button
                        type="button"
                        onClick={() => handleOpenCommunicationsMessage(m)}
                        style={subtleBtnStyle}
                        aria-label={`View thread in communications`}
                      >
                        <ExternalLink size={11} /> View thread
                      </button>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        </div>
      )}
    </section>
  )
}
