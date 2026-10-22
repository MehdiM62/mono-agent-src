// Client-generated turn id (plan: "Client-created turn ID: registered
// locally before Start"). crypto.randomUUID() is gated on a secure
// context — Wails' custom-scheme webview origin isn't guaranteed to
// qualify on every platform the way http://wails.localhost does on
// Windows, so relying on it directly would make every send() throw on
// whichever platforms don't. crypto.getRandomValues has no such
// restriction, so build an RFC 4122 v4 string from it whenever
// randomUUID isn't available instead of failing outright. Exported for
// direct testing (jsdom/Node's crypto always has randomUUID, so the
// fallback path needs to be exercised explicitly).
export function newTurnId() {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = [...bytes].map(b => b.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
