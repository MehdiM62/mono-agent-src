import React from 'react'

export const STATUS_COLORS = {
  pending: { bg: 'rgba(245,158,11,0.12)', color: '#f59e0b', label: 'pending' },
  applied: { bg: 'rgba(16,185,129,0.12)', color: '#10b981', label: 'applied' },
  rejected: { bg: 'rgba(239,68,68,0.12)', color: '#ef4444', label: 'rejected' },
  cancelled: { bg: 'rgba(100,116,139,0.12)', color: '#64748b', label: 'cancelled' },
  awaiting_submit: { bg: 'rgba(0,180,216,0.15)', color: '#00b4d8', label: 'awaiting submit' },
  needs_you: { bg: 'rgba(244,63,94,0.15)', color: '#f43f5e', label: 'needs you' },
}

export function StatusBadge({ status, style = {} }) {
  const norm = (status || '').toLowerCase().trim()
  const s = STATUS_COLORS[norm] || { bg: '#1a2332', color: '#64748b', label: status || 'unknown' }
  const displayLabel = s.label || norm.replace(/_/g, ' ')
  return (
    <span
      role="status"
      aria-label={`Status: ${displayLabel}`}
      style={{
        background: s.bg,
        border: `1px solid ${s.color}33`,
        borderRadius: 4,
        padding: '2px 8px',
        fontFamily: 'var(--font-mono)',
        fontSize: 10,
        fontWeight: 600,
        color: s.color,
        display: 'inline-flex',
        alignItems: 'center',
        textTransform: 'uppercase',
        letterSpacing: '0.04em',
        ...style,
      }}
    >
      {displayLabel}
    </span>
  )
}

export function fmtBytes(bytes) {
  const b = Number(bytes)
  if (isNaN(b) || b <= 0) return '0 B'
  if (b < 1024) return b + ' B'
  if (b < 1024 * 1024) return (b / 1024).toFixed(1) + ' KB'
  return (b / (1024 * 1024)).toFixed(1) + ' MB'
}

export const sectionCardStyle = {
  background: 'rgba(255,255,255,0.02)',
  border: '1px solid rgba(255,255,255,0.07)',
  borderRadius: 8,
  padding: 14,
  display: 'flex',
  flexDirection: 'column',
  gap: 10,
}

export const sectionTitleStyle = {
  margin: 0,
  fontSize: 12,
  fontWeight: 600,
  textTransform: 'uppercase',
  letterSpacing: '0.06em',
  color: 'var(--text-secondary, #94a3b8)',
  display: 'flex',
  alignItems: 'center',
  gap: 6,
}

export const subtleBtnStyle = {
  background: 'rgba(0,180,216,0.1)',
  border: '1px solid rgba(0,180,216,0.3)',
  borderRadius: 5,
  padding: '4px 10px',
  color: '#00b4d8',
  fontFamily: 'var(--font-mono)',
  fontSize: 11,
  cursor: 'pointer',
  display: 'inline-flex',
  alignItems: 'center',
  gap: 5,
}
