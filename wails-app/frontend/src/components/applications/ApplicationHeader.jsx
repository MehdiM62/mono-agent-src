import React from 'react'
import { ExternalLink, Clock, X, Building2, Calendar } from 'lucide-react'
import * as WailsApp from '../../wailsjs/go/main/App'
import { StatusBadge, subtleBtnStyle } from './constants.jsx'
import SendToCoderButton from '../SendToCoderButton.jsx'
import ApplyWithCoderButton from '../ApplyWithCoderButton.jsx'
import { selectionText } from '../SendToCoderApi.js'

export default function ApplicationHeader({ detail, assets, onSetStatus, onClose }) {
  if (!detail) return null

  const title = detail.title || 'Untitled Application'
  const orgName = detail.kind === 'job'
    ? (detail.company || 'Unknown Company')
    : (detail.issuing_org || detail.company || 'Unknown Organization')

  const deadline = detail.submission_deadline || detail.submissionDeadline || assets?.posting?.deadline
  const postingUrl = detail.url || assets?.posting?.url
  const canCancel = ['pending', 'awaiting_submit', 'needs_you'].includes(detail.status)

  const handleOpenUrl = (e) => {
    e.preventDefault()
    if (postingUrl) {
      if (typeof WailsApp.OpenURL === 'function') {
        WailsApp.OpenURL(postingUrl)
      } else {
        window.open(postingUrl, '_blank', 'noopener,noreferrer')
      }
    }
  }

  return (
    <header
      role="banner"
      aria-label="Application header"
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 10,
        paddingBottom: 14,
        borderBottom: '1px solid rgba(255,255,255,0.08)',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12 }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 0, flex: 1 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
            <h2
              style={{
                margin: 0,
                fontSize: 18,
                fontWeight: 600,
                color: 'var(--text-primary, #f1f5f9)',
                lineHeight: 1.3,
                wordBreak: 'break-word',
              }}
            >
              {title}
            </h2>
            <StatusBadge status={detail.status} />
          </div>

          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 12,
              flexWrap: 'wrap',
              fontSize: 12,
              color: 'var(--text-secondary, #94a3b8)',
              fontFamily: 'var(--font-mono)',
            }}
          >
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
              <Building2 size={13} style={{ color: '#64748b' }} />
              {orgName}
            </span>

            <span style={{ color: 'rgba(255,255,255,0.2)' }}>·</span>

            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
              <Clock size={13} style={{ color: '#64748b' }} />
              {deadline ? `Deadline: ${deadline}` : 'No deadline'}
            </span>

            {postingUrl && (
              <>
                <span style={{ color: 'rgba(255,255,255,0.2)' }}>·</span>
                <a
                  href={postingUrl}
                  onClick={handleOpenUrl}
                  title="Open application URL"
                  aria-label={`Open link: ${postingUrl}`}
                  style={{
                    color: '#00b4d8',
                    textDecoration: 'none',
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 4,
                    maxWidth: 240,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  <ExternalLink size={12} />
                  <span>{postingUrl}</span>
                </a>
              </>
            )}
          </div>
        </div>

        <div style={{ display: 'flex', alignItems: 'flex-start', gap: 8, flexShrink: 0, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
          {canCancel && <ApplyWithCoderButton applicationId={detail.id} />}
          <SendToCoderButton label="Discuss in coder" goal="Discuss this application with me: its fit, the posting, and what to do next."
            applicationId={detail.id} origin={{ page: 'applications', label: 'Applications' }}
            context={[selectionText([title, orgName, postingUrl].filter(Boolean).join(' | '))]} />
          {canCancel && onSetStatus && (
            <button
              type="button"
              onClick={() => onSetStatus(detail.id, 'cancelled')}
              style={{
                ...subtleBtnStyle,
                color: '#f43f5e',
                borderColor: 'rgba(244,63,94,0.3)',
                background: 'rgba(244,63,94,0.1)',
              }}
              aria-label="Cancel this application"
            >
              Cancel Application
            </button>
          )}

          {onClose && (
            <button
              type="button"
              onClick={onClose}
              aria-label="Close detail modal"
              style={{
                background: 'transparent',
                border: 'none',
                color: 'var(--text-muted, #64748b)',
                cursor: 'pointer',
                padding: 4,
                borderRadius: 4,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
              }}
            >
              <X size={18} />
            </button>
          )}
        </div>
      </div>
    </header>
  )
}
