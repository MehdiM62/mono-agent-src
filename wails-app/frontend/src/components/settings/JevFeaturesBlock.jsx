import { useState, useEffect } from 'react'
import { ChevronRight, ChevronDown, ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { mono, label, hint, errText, input, formatUSD, Switch } from './jevShared.jsx'

function EgressList({ items }) {
  if (!items?.length) return <div style={hint}>No details reported by the CLI.</div>
  return (
    <ul style={{ margin: 0, paddingLeft: 18, display: 'flex', flexDirection: 'column', gap: 2 }}>
      {items.map((e, i) => (
        <li key={i} style={{ fontFamily: 'var(--font-body)', fontSize: 11, color: 'var(--text-secondary)', lineHeight: 1.5 }}>{e}</li>
      ))}
    </ul>
  )
}

function ThresholdInput({ value, disabled, onChange, id }) {
  return (
    <input
      id={id} type="number" min={0.05} max={1} step={0.05} value={value} disabled={disabled}
      onChange={e => onChange(e.target.value)}
      style={{ ...input, width: 76, padding: '4px 8px' }}
    />
  )
}

// 0.05–1, or the surface's own default (people_review defaults to 0: suggest only).
const validThreshold = (v, def) => {
  const n = Number(v)
  return v !== '' && Number.isFinite(n) && ((n >= 0.05 && n <= 1) || n === def)
}

function SurfaceRow({ s, busy, pending, onStartEnable, onCancelEnable, onConfirmEnable, onDisable, onThreshold }) {
  const [open, setOpen] = useState(false)
  const [thr, setThr] = useState(String(s.threshold ?? s.default_threshold ?? ''))
  useEffect(() => { setThr(String(s.threshold ?? s.default_threshold ?? '')) }, [s.threshold, s.default_threshold, s.enabled])
  const title = s.title || s.surface
  const changed = s.enabled && validThreshold(thr, s.default_threshold) && Number(thr) !== s.threshold
  const thrId = `jev-thr-${s.surface}`

  return (
    <div data-jev-surface={s.surface} style={{
      borderTop: '1px solid var(--border-dim)', paddingTop: 10, display: 'flex', flexDirection: 'column', gap: 6,
    }}>
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: 12 }}>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, flexWrap: 'wrap' }}>
            <span style={{ fontFamily: mono, fontSize: 12, fontWeight: 600, color: 'var(--text)' }}>{title}</span>
            <span style={{ fontFamily: mono, fontSize: 9.5, color: 'var(--text-dim)' }}>{s.surface}</span>
          </div>
          {s.description && <div style={{ ...hint, marginTop: 2 }}>{s.description}</div>}
        </div>
        {s.enabled && (
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexShrink: 0 }}>
            <label htmlFor={thrId} style={{ ...label, letterSpacing: 0.5 }}>Threshold</label>
            <ThresholdInput id={thrId} value={thr} disabled={busy} onChange={setThr} />
            {changed && (
              <button className="btn btn-secondary btn-sm" disabled={busy} onClick={() => onThreshold(s.surface, Number(thr))}>Apply</button>
            )}
          </div>
        )}
        <Switch
          on={s.enabled || pending} disabled={busy} label={`${title}: ${s.enabled ? 'on' : 'off'}`}
          onChange={on => (on ? onStartEnable(s.surface) : (pending ? onCancelEnable() : onDisable(s.surface)))}
        />
      </div>

      {pending ? (
        <div data-testid={`jev-confirm-${s.surface}`} style={{
          background: 'rgba(234,179,8,.05)', border: '1px solid rgba(234,179,8,.22)', borderRadius: 'var(--radius)',
          padding: '10px 12px', display: 'flex', flexDirection: 'column', gap: 8,
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, fontFamily: mono, fontSize: 11, color: 'var(--yellow)' }}>
            <ShieldAlert size={13} /> Turning this on sends the following to TypeSafe:
          </div>
          <EgressList items={s.egress} />
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
            <label htmlFor={thrId} style={{ ...label, letterSpacing: 0.5 }}>Threshold</label>
            <ThresholdInput id={thrId} value={thr} disabled={busy} onChange={setThr} />
            <span style={{ ...hint, flex: 1, minWidth: 140 }}>Jev only acts when its top answer is at least this likely.</span>
            <button className="btn btn-secondary btn-sm" disabled={busy} onClick={onCancelEnable}>Cancel</button>
            <button className="btn btn-primary btn-sm" disabled={busy || !validThreshold(thr, s.default_threshold)}
              onClick={() => onConfirmEnable(s.surface, Number(thr))}>
              Send this and enable
            </button>
          </div>
        </div>
      ) : (
        <div>
          <button
            type="button" onClick={() => setOpen(o => !o)} aria-expanded={open}
            style={{ background: 'none', border: 'none', padding: 0, cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: 4, fontFamily: mono, fontSize: 10, color: 'var(--cyan-dim)' }}
          >
            {open ? <ChevronDown size={12} /> : <ChevronRight size={12} />} What is sent to TypeSafe
          </button>
          {open && <div style={{ marginTop: 6 }}><EgressList items={s.egress} /></div>}
        </div>
      )}
    </div>
  )
}

function UsageTable({ usage, titles }) {
  const rows = usage?.surfaces || []
  if (!rows.length) return <div style={hint}>No Jev calls in the last 7 days.</div>
  const th = { ...label, fontSize: 9, textAlign: 'right', padding: '0 0 6px 12px', fontWeight: 400 }
  const td = { fontFamily: mono, fontSize: 11, color: 'var(--text-secondary)', textAlign: 'right', padding: '4px 0 4px 12px', whiteSpace: 'nowrap' }
  const row = (u, total) => (
    <tr key={u.surface} style={total ? { borderTop: '1px solid var(--border)' } : undefined}>
      <td style={{ ...td, textAlign: 'left', paddingLeft: 0, color: total ? 'var(--text)' : 'var(--text-secondary)', whiteSpace: 'normal' }}>
        {total ? 'Total' : (titles[u.surface] || u.surface)}
      </td>
      <td style={td}>{u.calls}{u.failures ? <span style={{ color: 'var(--red)' }}> ({u.failures} failed)</span> : null}</td>
      <td style={td}>{(u.input_tokens || 0).toLocaleString()}</td>
      <td style={td}>{formatUSD(u.estimated_usd)}</td>
    </tr>
  )
  return (
    <div style={{ overflowX: 'auto' }}>
      <table style={{ width: '100%', maxWidth: 640, borderCollapse: 'collapse' }}>
        <thead>
          <tr>
            <th style={{ ...th, textAlign: 'left', paddingLeft: 0 }}>Feature</th>
            <th style={th}>Calls</th>
            <th style={th}>Input tokens</th>
            <th style={th}>Est. cost</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(u => row(u, false))}
          {rows.length > 1 && usage.total && row(usage.total, true)}
        </tbody>
      </table>
    </div>
  )
}

// The per-profile half: the consent note (profile part only), the feature
// toggles with their thresholds and the 7-day usage table.
export default function JevFeaturesBlock({ part, status, usage, usageErr, busy, pending, onPending, onClearErr, onSetSurface }) {
  const { t } = useTranslation()
  const surfaces = status.surfaces || []
  const titles = Object.fromEntries(surfaces.map(s => [s.surface, s.title || s.surface]))
  const hasKey = status.key_source && status.key_source !== 'none'
  const disabled = !!busy

  return (
    <>
      {part === 'profile' && <div style={hint}>{t('settings.jev.consent')}</div>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        <span style={label}>Features</span>
        {!hasKey && surfaces.some(s => s.enabled) && (
          <div style={{ ...hint, color: 'var(--yellow)' }}>Enabled features stay inactive until a key is set.</div>
        )}
        {surfaces.length === 0 && <div style={hint}>This version of monoagentcli reports no Jev features.</div>}
        {surfaces.map(s => (
          <SurfaceRow
            key={s.surface} s={s} busy={disabled} pending={pending === s.surface}
            onStartEnable={name => { onClearErr(); onPending(name) }}
            onCancelEnable={() => onPending('')}
            onConfirmEnable={(name, thr) => onSetSurface(name, true, thr)}
            onDisable={name => onSetSurface(name, false, 0)}
            onThreshold={(name, thr) => onSetSurface(name, true, thr)}
          />
        ))}
      </div>

      {/* Usage */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8, borderTop: '1px solid var(--border-dim)', paddingTop: 12 }}>
        <span style={label}>Usage · last 7 days</span>
        {usageErr ? <div style={errText}>Couldn't read usage: {usageErr}</div> : <UsageTable usage={usage} titles={titles} />}
      </div>
    </>
  )
}
