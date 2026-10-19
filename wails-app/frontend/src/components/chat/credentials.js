// Credential-shaped values in a command line, a tool's input or its output
// are masked wherever a card, the stage or a banner shows them (#872
// review). This is best effort: it catches the names and shapes below, not
// every secret. The guarantee for MonoAgent's own secret commands is that
// their cards show nothing at all (vaultRelated in toolLinks.js).

// The words that make a key's name a credential's, in one place. These
// count anywhere in the name (max_tokens, GITHUB_TOKEN, clientSecret,
// sessionId, cardNumber); WHOLE_WORDS only as a word of it (apiKey,
// x-api-key, basic_auth, db_pass, admin_pw), so author, keyboard, path,
// spinner and classname stay as they are. Every key internal/workflow/
// redact.go masks is one; credentials.test.js holds that.
export const CREDENTIAL_WORDS = [
  'token', 'secret', 'password', 'passwd', 'passphrase', 'authorization', 'bearer', 'cookie', 'session', 'credential',
  'apikey', 'accesskey', 'privatekey', 'licensekey', 'sshkey', 'iban', 'cardnumber', 'cvv', 'csrf', 'xsrf', 'phpsessid', 'dsn',
]
export const WHOLE_WORDS = ['key', 'auth', 'pass', 'pwd', 'pw', 'pswd', 'otp', 'pin', 'pat', 'sig', 'signature', 'sid', 'ssn']
const WHOLE = new Set(WHOLE_WORDS)

export const MASK = '•••'

// isCredentialKey says whether a flag, parameter or key name holds a
// credential, read with %XX decoded (api%5Fkey).
export function isCredentialKey(key) {
  const name = String(key).replace(/%([0-9A-Fa-f]{2})/g, (_, h) => String.fromCharCode(parseInt(h, 16)))
  const flat = name.toLowerCase().replace(/[^a-z0-9]/g, '')
  if (CREDENTIAL_WORDS.some(w => flat.includes(w))) return true
  return name.replace(/([a-z0-9])([A-Z])/g, '$1_$2').toLowerCase().split(/[^a-z0-9]+/).some(w => WHOLE.has(w))
}

// An on/off switch is never masked (ssh's -o StrictHostKeyChecking with
// no, a JSON "secure" with true).
const PLAIN = /^(?:\\?["'])?(?:yes|no|true|false|null)(?:\\?["'])?$/i

// The pieces the rules are made of. A gap between a flag and its value may
// be white space, a backslash-newline, or both written as JSON escapes (the
// raw view of arguments cut short shows \n as two characters).
const NAME = '(?:[A-Za-z_]|%[0-9A-Fa-f]{2})(?:[A-Za-z0-9_.-]|%[0-9A-Fa-f]{2})*'
const GAP = '(?:\\s|\\\\[ntr]|\\\\(?=\\s)|\\\\\\\\(?=\\\\[ntr]))+'
const EDGE = '(?:^|\\\\[ntr]|[\\s"\'(;&|?{,=:])'
const VALUE = [
  '\\\\"(?:[^"\\\\]|\\\\[^"])*(?:\\\\")?', // \"…\" inside a JSON string
  '"(?:[^"\\\\]|\\\\.)*"?', // "…", to its end if cut short
  "'[^']*'?",
  '(?:[^\\s\'";&|\\\\]|\\\\(?![ntr"]))+', // a bare word
].join('|')
const SCHEMES = '(?:Bearer|Basic|Token)'
// The rest of a header inside its quotes.
const HEADER_REST = '(?:[^"\'\\n\\\\]|\\\\(?![n"\']))+'
const rule = (re, flags, keyOk = isCredentialKey) => ({ re: new RegExp(re, flags), keyOk })
const KEY_RULES = [
  // JSON: "key": "value" (a cut-short value runs to the end).
  rule(`(?<head>"(?<key>${NAME})"\\s*:\\s*")(?<value>(?:[^"\\\\]|\\\\.)*)`, 'g'),
  // JSON inside a JSON string: \"key\": \"value\".
  rule(`(?<head>\\\\"(?<key>${NAME})\\\\"\\s*:\\s*\\\\")(?<value>(?:[^"\\\\]|\\\\[^"])*)`, 'g'),
  // A dict with single quotes: 'key': 'value'.
  rule(`(?<head>'(?<key>${NAME})'\\s*:\\s*')(?<value>[^']*)`, 'g'),
  // A JSON key with a bare value: "key": 12345.
  rule(`(?<head>\\\\?"(?<key>${NAME})\\\\?"\\s*:\\s*)(?<value>[^\\s"',}\\]\\\\][^\\s,}\\]\\\\]*)`, 'g'),
  // A header flag: -H 'Authorization: Digest …', -H "auth: K"; the rest of
  // the header is masked, the scheme kept.
  rule(`(?<head>(?:-H|--header)(?:${GAP}|=)\\\\?["'](?<key>${NAME})\\s*:\\s*(?:${SCHEMES}\\s+)?)(?<value>${HEADER_REST})`, 'gi'),
  // A header flag whose value is the next word: -H 'X-Api-Key:' K.
  rule(`(?<head>(?:-H|--header)(?:${GAP}|=)\\\\?["'](?<key>${NAME}):\\s*\\\\?["']${GAP})(?<value>${VALUE})`, 'gi'),
  // A flag: --api-key K, --token=K, --api-key \<newline> K.
  rule(`(?<head>(?:^|\\\\[ntr]|[\\s"'(])--?(?<key>${NAME})(?:=|${GAP}))(?!-)(?<value>${VALUE})`, 'g'),
  // jq's --arg name value.
  rule(`(?<head>--arg(?:json)?${GAP}(?<key>${NAME})${GAP})(?<value>${VALUE})`, 'g'),
  // npm config set //registry/:_authToken K, and the like.
  rule(`(?<head>\\bconfig\\s+set\\s+(?<key>[^\\s=]+)\\s+)(?<value>${VALUE})`, 'g'),
  // A key=value: API_KEY=K, ?access_token=K, process.env.TOKEN='K',
  // --from-literal=pw=K, :_authToken=K.
  rule(`(?<head>${EDGE}(?<key>${NAME})=)(?<value>${VALUE})`, 'g'),
  // A header or a YAML line: Authorization: Bearer K, X-Api-Key: K. A bare
  // "auth:" outside a header flag is left alone: it is a commit message's
  // scope far more often.
  rule(`(?<head>(?:^|\\\\[ntr]|[\\s"'])(?<key>${NAME})\\s*:\\s*(?:${SCHEMES}\\s+)?)(?<value>[^\\s"',;\\\\]+)`, 'gi', k => isCredentialKey(k) && k.toLowerCase() !== 'auth'),
]

// Credentials that sit in a command's own syntax rather than after a name.
const ARG_RULES = [
  // curl -u user:<value>, --user, -uuser:<value> (not a URL: redis-cli -u
  // redis://… has its own rule).
  /(?<head>(?:^|\s)(?:-u|--user|--proxy-user)(?:\s+|=)?(?:\\?["'])?[^\s:"'\\/]*:)(?!\/\/)(?<value>[^\s"'\\]+)/g,
  // mysql -p<value> (with a space, -p asks for it).
  /(?<head>\b(?:mysql|mysqldump|mysqladmin|mariadb)\b[^|;&\n]*?\s-p)(?<value>[^\s'"\\-][^\s'"\\]*)/g,
  // sshpass -p <value>, docker login -p <value>.
  new RegExp(`(?<head>\\bsshpass\\b[^|;&\\n]*?\\s-p\\s*)(?<value>${VALUE})`, 'g'),
  new RegExp(`(?<head>\\b(?:docker|podman|nerdctl|buildah)\\s+login\\b[^|;&\\n]*?\\s-p(?:${GAP}|=))(?<value>${VALUE})`, 'g'),
  // openssl's pass:<value>.
  /(?<head>(?:^|[\s"'=])pass:)(?<value>[^\s"'\\]+)/g,
]

// Shapes masked wherever they stand, whatever names them, in input and in
// output alike. Long hex strings and long base64 are left alone: they would
// mask commit hashes and ordinary data.
const TOKEN_SHAPES = [
  '\\bsk-[A-Za-z0-9_-]{16,}', // OpenAI, Anthropic and similar API keys
  '\\b(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{16,}', // Stripe secret and restricted keys
  '\\bwhsec_[A-Za-z0-9]{16,}', // Stripe webhook signing keys
  '\\bgh[pousr]_[A-Za-z0-9]{16,}', // GitHub tokens
  '\\bgithub_pat_[A-Za-z0-9_]{16,}',
  '\\bglpat-[A-Za-z0-9_-]{16,}', // GitLab
  '\\bnpm_[A-Za-z0-9]{30,}', // npm
  '\\bxox[bpas]-[A-Za-z0-9-]{8,}', // Slack tokens
  '\\bSG\\.[A-Za-z0-9_-]{16,}\\.[A-Za-z0-9_-]{16,}', // SendGrid
  '\\bSK[A-Za-z0-9]{32}\\b', // Twilio API keys
  '\\bshp(?:at|ss|ca|pa)_[A-Za-z0-9]{20,}', // Shopify
  '\\bdo[pora]_v1_[A-Za-z0-9]{40,}', // DigitalOcean
  '(?<![0-9])[0-9]{8,10}:AA[A-Za-z0-9_-]{30,}', // Telegram bot tokens
  '\\bkey-[A-Za-z0-9]{32}\\b', // Mailgun
  '\\bAKIA[0-9A-Z]{16}\\b', // AWS access key ids
  '\\bAIza[0-9A-Za-z_-]{30,}', // Google API keys
  '\\beyJ[A-Za-z0-9_-]+\\.eyJ[A-Za-z0-9_-]+(?:\\.[A-Za-z0-9_-]*)?', // JWTs
].map(s => new RegExp(s, 'g'))
// Shapes whose head stays: the secret part of a webhook URL, a URL's
// password (https://u:<pw>@, postgres://, redis://:<pw>@, mongodb+srv://…),
// what follows Bearer (any length), and Basic and Token credentials.
const KEPT_HEAD_SHAPES = [
  /(?<head>hooks\.slack\.com\/(?:services|workflows|triggers)\/)[A-Za-z0-9/_-]+/g,
  /(?<head>discord(?:app)?\.com\/api\/webhooks\/[0-9]+\/)[A-Za-z0-9_-]+/g,
  /(?<head>\b[a-z][a-z0-9+.-]*:\/\/[^\s/:@'"\\]*:)[^\s@/'"\\]+(?=@)/gi,
  /(?<head>\bBearer\s+)(?!•••)[^\s"',;\\]+/gi,
  /(?<head>\b(?:Basic|Token)\s+)[A-Za-z0-9._~+/=-]{8,}/g,
]
// A private key block, PEM or OpenSSH, to its end (or the text's, if cut).
const PRIVATE_KEY = /-{5}BEGIN [A-Z0-9 ]*PRIVATE KEY-{5}[\s\S]*?(?:-{5}END [A-Z0-9 ]*PRIVATE KEY-{5}|$)/g

// maskSecretShapes masks the token shapes alone: what output gets.
export function maskSecretShapes(text) {
  if (typeof text !== 'string' || !text) return text
  let out = text.replace(PRIVATE_KEY, MASK)
  for (const re of KEPT_HEAD_SHAPES) out = out.replace(re, (...a) => a.at(-1).head + MASK)
  for (const re of TOKEN_SHAPES) out = out.replace(re, MASK)
  return out
}

// maskCredentials returns text with each credential-shaped value masked:
// the shapes, the values after a credential's name, and the values in
// curl -u, mysql -p, sshpass, docker login and openssl's pass:.
export function maskCredentials(text) {
  if (typeof text !== 'string' || !text) return text
  let out = maskSecretShapes(text)
  for (const { re, keyOk } of KEY_RULES) {
    out = out.replace(re, (all, ...a) => {
      const { head, key, value } = a.at(-1)
      return keyOk(key) && value !== MASK && !PLAIN.test(value) ? head + MASK : all
    })
  }
  for (const re of ARG_RULES) {
    out = out.replace(re, (all, ...a) => {
      const { head, value } = a.at(-1)
      return value === MASK ? all : head + MASK
    })
  }
  return out
}

// maskArgs is a tool call's arguments with every string masked and every
// value under a credential key replaced, at any depth.
export function maskArgs(value, depth = 0) {
  if (typeof value === 'string') return maskCredentials(value)
  if (!value || typeof value !== 'object' || depth > 20) return value
  if (Array.isArray(value)) return value.map(v => maskArgs(v, depth + 1))
  return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, isCredentialKey(k) && v != null && v !== '' && !PLAIN.test(String(v)) ? MASK : maskArgs(v, depth + 1)]))
}
