// Credential-shaped values in a command line or a tool's input are masked
// wherever a card, the stage or a banner shows them (#872 review): the value
// of a flag, a query parameter, a JSON key or a key=value whose name looks
// like a credential, and a well-known token shape anywhere, read "•••".

// CREDENTIAL_WORDS are what makes a key's name a credential's. The long
// ones count anywhere in the name (max_tokens, GITHUB_TOKEN, clientSecret,
// sessionId); "key" and "auth" only as a word of it (apiKey, x-api-key,
// basic_auth), so author, keyboard and monkey stay as they are. Every key
// internal/workflow/redact.go masks is one; credentials.test.js holds that.
export const CREDENTIAL_WORDS = ['key', 'token', 'secret', 'password', 'passwd', 'auth', 'authorization', 'bearer', 'cookie', 'session', 'credential']
const WORD_ONLY = new Set(['key', 'auth'])
const ANYWHERE = CREDENTIAL_WORDS.filter(w => !WORD_ONLY.has(w)).concat(['apikey', 'accesskey', 'privatekey'])

export const MASK = '•••'

export function isCredentialKey(key) {
  const name = String(key)
  const flat = name.toLowerCase().replace(/[^a-z0-9]/g, '')
  if (ANYWHERE.some(w => flat.includes(w))) return true
  const words = name.replace(/([a-z0-9])([A-Z])/g, '$1_$2').toLowerCase().split(/[^a-z0-9]+/)
  return words.some(w => WORD_ONLY.has(w))
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
  // A key=value or a query parameter: API_KEY=K, ?access_token=K, --field password=K.
  new RegExp(`((?:^|[\\s"'(;&|?{,])(${NAME})=)(${SHELL_VALUE})`, 'g'),
  // A header or a YAML line: Authorization: Bearer K, X-Api-Key: K. A bare
  // "auth:" is left alone: it is a commit message's scope far more often.
  [new RegExp(`((?:^|[\\s"'])(${NAME})\\s*:\\s*(?:${SCHEMES}\\s+)?)([^\\s"',;]+)`, 'gi'), k => k.toLowerCase() !== 'auth'],
]

// Token shapes masked wherever they stand, whatever names them.
const TOKEN_SHAPES = [
  '\\bsk-[A-Za-z0-9_-]{16,}', // OpenAI, Anthropic and similar API keys
  '\\bgh[pousr]_[A-Za-z0-9]{16,}', // GitHub tokens
  '\\bgithub_pat_[A-Za-z0-9_]{16,}',
  '\\bxox[bpas]-[A-Za-z0-9-]{8,}', // Slack tokens
  '\\bAKIA[0-9A-Z]{16}\\b', // AWS access key ids
  '\\bAIza[0-9A-Za-z_-]{30,}', // Google API keys
  '\\beyJ[A-Za-z0-9_-]+\\.eyJ[A-Za-z0-9_-]+(?:\\.[A-Za-z0-9_-]*)?', // JWTs
].map(s => new RegExp(s, 'g'))
// Bearer and whatever follows it, of any length; Basic and Token
// credentials after their scheme.
const BEARER_VALUE = /\b(Bearer\s+)(?!•••)[^\s"',;]+/g
const SCHEME_VALUE = /\b((?:Basic|Token)\s+)[A-Za-z0-9._~+/=-]{8,}/g

// maskCredentials returns text with each credential-shaped value masked.
export function maskCredentials(text) {
  if (typeof text !== 'string' || !text) return text
  let out = text
  for (const rule of RULES) {
    const [re, also] = Array.isArray(rule) ? rule : [rule, () => true]
    out = out.replace(re, (all, head, key, value) => (isCredentialKey(key) && also(key) && value !== MASK ? head + MASK : all))
  }
  for (const re of TOKEN_SHAPES) out = out.replace(re, MASK)
  return out.replace(BEARER_VALUE, (all, head) => head + MASK).replace(SCHEME_VALUE, (all, head) => head + MASK)
}

// maskArgs is a tool call's arguments with every string masked and every
// value under a credential key replaced, at any depth.
export function maskArgs(value, depth = 0) {
  if (typeof value === 'string') return maskCredentials(value)
  if (!value || typeof value !== 'object' || depth > 20) return value
  if (Array.isArray(value)) return value.map(v => maskArgs(v, depth + 1))
  return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, isCredentialKey(k) && v != null && v !== '' ? MASK : maskArgs(v, depth + 1)]))
}
