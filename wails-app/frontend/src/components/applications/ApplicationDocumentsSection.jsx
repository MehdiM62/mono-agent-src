import React from 'react'
import { File, Eye, CheckCircle, UploadCloud, Sparkles } from 'lucide-react'
import { sectionCardStyle, sectionTitleStyle, subtleBtnStyle, fmtBytes } from './constants.jsx'

function DocumentKindBadge({ kind }) {
  const norm = (kind || '').toLowerCase()
  let label = 'Document'
  let color = '#94a3b8'

  if (norm === 'cv') {
    label = 'CV'
    color = '#00b4d8'
  } else if (norm === 'cover_letter') {
    label = 'Cover Letter'
    color = '#10b981'
  } else if (norm) {
    label = norm.replace(/_/g, ' ')
  }

  return (
    <span
      style={{
        background: 'rgba(255,255,255,0.06)',
        color,
        borderRadius: 3,
        padding: '1px 6px',
        fontSize: 10,
        fontFamily: 'var(--font-mono)',
        textTransform: 'uppercase',
      }}
    >
      {label}
    </span>
  )
}

export default function ApplicationDocumentsSection({ documents = [], onPreviewDoc }) {
  const docs = Array.isArray(documents) ? documents : []

  return (
    <section aria-labelledby="section-documents-title" style={sectionCardStyle}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
        <h3 id="section-documents-title" style={sectionTitleStyle}>
          <File size={14} style={{ color: '#00b4d8' }} />
          Documents ({docs.length})
        </h3>
        <span style={{ fontSize: 11, color: 'var(--text-muted, #64748b)', fontFamily: 'var(--font-mono)' }}>
          Generated & profile uploads
        </span>
      </div>

      {docs.length === 0 ? (
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
          No documents attached or generated for this application.
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          {docs.map((doc, idx) => {
            const isAttached = Boolean(doc.attached)
            const isGenerated = doc.source === 'generated'

            return (
              <div
                key={doc.id || doc.filename || idx}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 12,
                  padding: '8px 12px',
                  borderRadius: 6,
                  background: 'rgba(0,0,0,0.25)',
                  border: '1px solid rgba(255,255,255,0.05)',
                  fontSize: 12,
                }}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0, flex: 1 }}>
                  <File size={15} style={{ color: '#64748b', flexShrink: 0 }} />
                  <div style={{ display: 'flex', flexDirection: 'column', minWidth: 0, gap: 2 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
                      <span
                        style={{
                          fontFamily: 'var(--font-mono)',
                          color: 'var(--text-primary, #f1f5f9)',
                          fontWeight: 500,
                          wordBreak: 'break-all',
                        }}
                      >
                        {doc.filename || 'Untitled document'}
                      </span>
                      <DocumentKindBadge kind={doc.kind} />
                      {isAttached && (
                        <span
                          role="status"
                          aria-label="Attached to application"
                          style={{
                            background: 'rgba(16,185,129,0.15)',
                            border: '1px solid rgba(16,185,129,0.35)',
                            borderRadius: 3,
                            padding: '1px 6px',
                            color: '#10b981',
                            fontSize: 10,
                            fontFamily: 'var(--font-mono)',
                            display: 'inline-flex',
                            alignItems: 'center',
                            gap: 3,
                          }}
                        >
                          <CheckCircle size={10} /> Attached
                        </span>
                      )}
                    </div>
                    <div
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 8,
                        fontSize: 10,
                        color: 'var(--text-muted, #64748b)',
                        fontFamily: 'var(--font-mono)',
                      }}
                    >
                      <span>{fmtBytes(doc.sizeBytes)}</span>
                      <span>·</span>
                      <span style={{ textTransform: 'uppercase' }}>{doc.format || 'file'}</span>
                      <span>·</span>
                      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 3 }}>
                        {isGenerated ? <Sparkles size={10} style={{ color: '#00b4d8' }} /> : <UploadCloud size={10} />}
                        {isGenerated ? 'Generated' : 'Upload'}
                      </span>
                    </div>
                  </div>
                </div>

                <button
                  type="button"
                  onClick={() => onPreviewDoc?.(doc)}
                  style={subtleBtnStyle}
                  aria-label={`Preview ${doc.filename || 'document'}`}
                >
                  <Eye size={12} /> Preview
                </button>
              </div>
            )
          })}
        </div>
      )}
    </section>
  )
}
