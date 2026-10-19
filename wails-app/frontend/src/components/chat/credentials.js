// Credential-shaped values in a command line or a tool's input are masked
// wherever a card, the stage or a banner shows them: the value after a key
// such as api_key, token or password, in JSON, in flag or env form and in
// an Authorization header, reads "•••" (#872 review).

// CREDENTIAL_KEYS is internal/workflow/redact.go's redactKeyPattern, the
// list the Go redaction of tool arguments, run output and the HTTP API
// uses; credentials.test.js holds the two equal. A key matches when,
// lowercased with - read as _, it is one of these or ends in _ and one of
// them (GITHUB_TOKEN, --api-key, x-api-key).
export const CREDENTIAL_KEYS = [
  'password', 'passwd', 'secret', 'secrets', 'token', 'access_token', 'refresh_token', 'id_token', 'api_key', 'apikey',
  'authorization', 'auth', 'cookie', 'cookies', 'client_secret', 'private_key', 'bearer',
]

export const MASK = '•••'

const KEYS = new Set(CREDENTIAL_KEYS)

export function isCredentialKey(key) {
  const k = String(key).toLowerCase().replace(/-/g, '_')
  return KEYS.has(k) || CREDENTIAL_KEYS.some(c => k.endsWith(`_${c}`))
}

const NAME = '[A-Za-z_][A-Za-z0-9_-]*'
// A shell value: double- or single-quoted (to its end if cut short), or a
// bare word.
const SHELL_VALUE = `"(?:[^"\\\\]|\\\\.)*"?|'[^']*'?|[^\\s'";&|]+`
// The HTTP auth schemes kept in front of a masked header value.
const SCHEMES = '(?:Bearer|Basic|Token)'
const RULES = [
  // JSON: "key": "value" (a cut-short value runs to the end).
  new RegExp(`("(${NAME})"\\s*:\\s*")((?:[^"\\\\]|\\\\.)*)`, 'g'),
  // JSON inside a JSON string: \"key\": \"value\".
  new RegExp(`(\\\\"(${NAME})\\\\"\\s*:\\s*\\\\")((?:[^"\\\\]|\\\\[^"])*)`, 'g'),
  // A dict with single quotes: 'key': 'value'.
  new RegExp(`('(${NAME})'\\s*:\\s*')([^']*)`, 'g'),
  // A JSON key with a bare value: "key": 12345.
  new RegExp(`(\\\\?"(${NAME})\\\\?"\\s*:\\s*)([^\\s"',}\\]\\\\][^\\s,}\\]\\\\]*)`, 'g'),
  // A flag: --api-key K, --token=K.
  new RegExp(`((?:^|[\\s"'(])--?(${NAME})(?:=|\\s+))(?!-)(${SHELL_VALUE})`, 'g'),
  // An assignment: API_KEY=K, a URL's ?token=K, --field password=K.
  new RegExp(`((?:^|[\\s"'(;&|?{,])(${NAME})=)(${SHELL_VALUE})`, 'g'),
  // A header or a YAML line: Authorization: Bearer K, X-Api-Key: K. A bare
  // "auth:" is left alone: it is a commit message's scope far more often.
  [new RegExp(`((?:^|[\\s"'])(${NAME})\\s*:\\s*(?:${SCHEMES}\\s+)?)([^\\s"',;]+)`, 'gi'), k => k.toLowerCase() !== 'auth'],
]
// An auth scheme's credential standing alone, without its header.
const SCHEME_VALUE = new RegExp(`\\b(${SCHEMES}\\s+)([A-Za-z0-9._~+/=-]{8,})`, 'g')

// maskCredentials returns text with each credential-shaped value masked.
export function maskCredentials(text) {
  if (typeof text !== 'string' || !text) return text
  let out = text
  for (const rule of RULES) {
    const [re, also] = Array.isArray(rule) ? rule : [rule, () => true]
    out = out.replace(re, (all, head, key, value) => (isCredentialKey(key) && also(key) && value !== MASK ? head + MASK : all))
  }
  return out.replace(SCHEME_VALUE, (all, head, value) => (value === MASK ? all : head + MASK))
}

// maskArgs is a tool call's arguments with every string masked and every
// value under a credential key replaced, at any depth.
export function maskArgs(value, depth = 0) {
  if (typeof value === 'string') return maskCredentials(value)
  if (!value || typeof value !== 'object' || depth > 20) return value
  if (Array.isArray(value)) return value.map(v => maskArgs(v, depth + 1))
  return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, isCredentialKey(k) && v != null && v !== '' ? MASK : maskArgs(v, depth + 1)]))
}
