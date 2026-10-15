import { AsciiHost, Section, box, requesterParts } from './BrowserSubmitDetails.jsx'

// A browser site approval (plan C8): an agent whose kind is at "Ask per new
// site" tried to use a site with no rule. readonly_data is {origin, url,
// kind, requested_by, asciiHost?}. Approving it (Allow) adds an allow rule for the
// origin, which every agent kind then honours; rejecting it (Not now) adds
// nothing.

export const BROWSER_SITE = 'browser.site'

export function isBrowserSite(item) {
  return item?.node_config?.kind === BROWSER_SITE
}

const WHO = { coder: 'A coder chat', org: 'An org role', chat: 'A chat', other: 'An agent' }

export function BrowserSiteDetails({ item }) {
  const ro = item.readonly_data || {}
  const who = requesterParts(ro.requested_by)
  return (
    <div data-testid="browser-site-details" style={{ marginBottom: 20 }}>
      <div style={{ fontSize: 14, color: '#e2e8f0', marginBottom: 12, wordBreak: 'break-all' }}>
        {WHO[ro.kind] || 'An agent'} wants to use <strong>{ro.origin || 'a site'}</strong>
        <AsciiHost host={ro.asciiHost} />
      </div>
      {ro.url && (
        <Section label="Page">
          <div style={{ ...box, fontFamily: 'var(--font-mono)', fontSize: 11 }}>{ro.url}</div>
        </Section>
      )}
      <Section label="Requested by">
        <div style={box}>{who.length ? who.join(' · ') : 'an agent (it did not say which)'}</div>
      </Section>
      <div style={{ fontSize: 11, color: 'var(--text-muted)', lineHeight: 1.5 }}>
        Allow adds an allow rule for this site, so agents can use it from now on (Settings → Agent Browser
        Permissions lists and removes rules). Not now leaves it off limits.
      </div>
    </div>
  )
}
