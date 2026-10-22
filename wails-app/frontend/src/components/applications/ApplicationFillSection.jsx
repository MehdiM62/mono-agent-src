import React, { useState } from 'react'
import { CheckSquare, Image as ImageIcon, Sparkles, AlertCircle } from 'lucide-react'
import { sectionCardStyle, sectionTitleStyle, subtleBtnStyle } from './constants.jsx'

function FillStatusBadge({ status }) {
  const norm = (status || 'none').toLowerCase()
  let bg = 'rgba(100,116,139,0.15)'
  let color = '#94a3b8'

  if (norm === 'filled') {
    bg = 'rgba(16,185,129,0.15)'
    color = '#10b981'
  } else if (norm === 'filling') {
    bg = 'rgba(0,180,216,0.15)'
    color = '#00b4d8'
  } else if (norm === 'needs_you') {
    bg = 'rgba(244,63,94,0.15)'
    color = '#f43f5e'
  } else if (norm === 'submitted') {
    bg = 'rgba(168,85,247,0.15)'
    color = '#c084fc'
  }

  return (
    <span
      role="status"
      aria-label={`Fill status: ${norm}`}
      style={{
        background: bg,
        border: `1px solid ${color}33`,
        borderRadius: 4,
        padding: '2px 8px',
        color,
        fontSize: 10,
        fontFamily: 'var(--font-mono)',
        fontWeight: 600,
        textTransform: 'uppercase',
      }}
    >
      {norm.replace(/_/g, ' ')}
    </span>
  )
}

// On a narrow dialog each field stacks: label over value, then its source.
const STACK_CSS = `@media (max-width: 560px) {
  .fill-report thead { position: absolute; left: -9999px; }
  .fill-report, .fill-report tbody, .fill-report tr, .fill-report td { display: block !important; width: 100% !important; max-width: none !important; box-sizing: border-box; }
  .fill-report td { padding: 2px 8px !important; text-align: left !important; overflow-wrap: anywhere; }
  .fill-report tr { padding: 6px 0; }
}`

export default function ApplicationFillSection({ fill }) {
  const [showScreenshot, setShowScreenshot] = useState(true)

  const hasFields = Array.isArray(fill?.fields) && fill.fields.length > 0
  const hasScreenshot = Boolean(fill?.screenshot)
  const isEmpty = !fill || (!hasFields && !hasScreenshot)

  return (
    <section aria-labelledby="section-fill-title" style={sectionCardStyle}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 8 }}>
        <h3 id="section-fill-title" style={sectionTitleStyle}>
          <CheckSquare size={14} style={{ color: '#00b4d8' }} />
          Filled Form
        </h3>

        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {fill?.status && <FillStatusBadge status={fill.status} />}
          {fill?.updatedAt && (
            <span style={{ fontSize: 10, color: 'var(--text-muted, #64748b)', fontFamily: 'var(--font-mono)' }}>
              Updated: {fill.updatedAt.slice(0, 16).replace('T', ' ')}
            </span>
          )}
        </div>
      </div>

      {fill?.note && (
        <div
          style={{
            padding: '8px 10px',
            borderRadius: 5,
            background: 'rgba(0,180,216,0.06)',
            border: '1px solid rgba(0,180,216,0.18)',
            fontSize: 11,
            color: 'var(--text-secondary, #94a3b8)',
            fontFamily: 'var(--font-mono)',
          }}
        >
          {fill.note}
        </div>
      )}

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
          No filled form data available.
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          {/* Screenshot preview */}
          {hasScreenshot && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                <span style={{ fontSize: 11, color: 'var(--text-muted, #64748b)', fontFamily: 'var(--font-mono)', display: 'inline-flex', alignItems: 'center', gap: 4 }}>
                  <ImageIcon size={12} /> Form Screenshot
                </span>
                <button
                  type="button"
                  onClick={() => setShowScreenshot(!showScreenshot)}
                  style={{
                    background: 'transparent',
                    border: 'none',
                    color: '#00b4d8',
                    cursor: 'pointer',
                    fontSize: 10,
                    fontFamily: 'var(--font-mono)',
                  }}
                >
                  {showScreenshot ? 'Hide screenshot' : 'Show screenshot'}
                </button>
              </div>

              {showScreenshot && (
                <div
                  style={{
                    borderRadius: 6,
                    border: '1px solid rgba(255,255,255,0.08)',
                    background: '#04080f',
                    padding: 6,
                    display: 'flex',
                    justifyContent: 'center',
                    maxHeight: 280,
                    overflow: 'hidden',
                  }}
                >
                  <img
                    src={fill.screenshot}
                    alt="Filled form screenshot"
                    style={{
                      maxWidth: '100%',
                      maxHeight: 268,
                      objectFit: 'contain',
                      borderRadius: 4,
                    }}
                  />
                </div>
              )}
            </div>
          )}

          {/* Field report table */}
          {hasFields && (
            <div style={{ overflowX: 'auto' }}>
              <style>{STACK_CSS}</style>
              <table
                className="fill-report"
                role="table"
                aria-label="Field report table"
                style={{
                  width: '100%',
                  borderCollapse: 'collapse',
                  fontFamily: 'var(--font-mono)',
                  fontSize: 11,
                  textAlign: 'left',
                }}
              >
                <thead>
                  <tr
                    style={{
                      borderBottom: '1px solid rgba(255,255,255,0.08)',
                      color: 'var(--text-muted, #64748b)',
                      fontSize: 10,
                      textTransform: 'uppercase',
                    }}
                  >
                    <th style={{ padding: '6px 8px' }}>Field</th>
                    <th style={{ padding: '6px 8px' }}>Value</th>
                    <th style={{ padding: '6px 8px' }}>Source</th>
                    <th style={{ padding: '6px 8px', textAlign: 'center' }}>Generated</th>
                  </tr>
                </thead>
                <tbody>
                  {fill.fields.map((f, i) => (
                    <tr
                      key={i}
                      style={{
                        borderBottom: '1px solid rgba(255,255,255,0.04)',
                        color: 'var(--text-primary, #e2e8f0)',
                      }}
                    >
                      <td style={{ padding: '6px 8px', fontWeight: 500, color: '#f1f5f9' }}>
                        {f.label}
                      </td>
                      <td
                        style={{
                          padding: '6px 8px',
                          color: 'var(--text-secondary, #cbd5e1)',
                          wordBreak: 'break-word',
                          maxWidth: 260,
                        }}
                      >
                        {f.value || <span style={{ color: 'var(--text-muted, #64748b)' }}>—</span>}
                      </td>
                      <td style={{ padding: '6px 8px', color: 'var(--text-muted, #94a3b8)' }}>
                        {f.source || '—'}
                      </td>
                      <td style={{ padding: '6px 8px', textAlign: 'center' }}>
                        {f.generated ? (
                          <span
                            role="status"
                            aria-label="Generated by AI"
                            style={{
                              background: 'rgba(168,85,247,0.15)',
                              border: '1px solid rgba(168,85,247,0.35)',
                              color: '#c084fc',
                              borderRadius: 3,
                              padding: '1px 6px',
                              fontSize: 9,
                              fontWeight: 600,
                              textTransform: 'uppercase',
                              display: 'inline-flex',
                              alignItems: 'center',
                              gap: 3,
                            }}
                          >
                            <Sparkles size={9} /> generated
                          </span>
                        ) : (
                          <span style={{ color: 'var(--text-muted, #475569)' }}>—</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </section>
  )
}
