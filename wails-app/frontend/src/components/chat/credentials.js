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

// Names that hold a credential word but never a credential: token limits
// and tokenizers, key and password files, key sizes, timeouts.
const NOT_CREDENTIAL = /^max[\W_]*\w*tokens?$|tokeni[sz]er|[-_.](?:file|path)$|[-_]bits$|timeout$/i

// isCredentialKey says whether a flag, parameter or key name holds a
// credential, read with %XX decoded (api%5Fkey).
export function isCredentialKey(key) {
  const name = String(key).replace(/%([0-9A-Fa-f]{2})/g, (_, h) => String.fromCharCode(parseInt(h, 16)))
  if (NOT_CREDENTIAL.test(name)) return false
  const flat = name.toLowerCase().replace(/[^a-z0-9]/g, '')
  if (CREDENTIAL_WORDS.some(w => flat.includes(w))) return true
  return name.replace(/([a-z0-9])([A-Z])/g, '$1_$2').toLowerCase().split(/[^a-z0-9]+/).some(w => WHOLE.has(w))
}

// An on/off switch is never masked (ssh's -o StrictHostKeyChecking with
// no, a JSON "secure" with true), nor a numeric session id.
const PLAIN = /^(?:\\?["'])?(?:yes|no|true|false|null)(?:\\?["'])?$/i
const plainValue = (key, value) => PLAIN.test(value) || (/session[-_]?id$/i.test(key) && /^\d+$/.test(value))
// The label keys of a name/value pair ({"name": "password", "value": …}):
// "key" there names the value, it is not one. A label is one when its
// object (the braces around it) has a value, val or v key too.
const LABEL_KEYS = /^(?:name|key|field|k)$/i
const VALUE_KEY = /(\\?["'])(?:value|val|v)\1\s*:/i
function inPair(text, at) {
  const open = text.lastIndexOf('{', at)
  const close = text.indexOf('}', at)
  return open >= 0 && VALUE_KEY.test(text.slice(open, close < 0 ? text.length : close))
}

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
  // npm config set //registry/:_authToken K, aws configure set
  // aws_secret_access_key K, and the like.
  rule(`(?<head>\\bconfig(?:ure)?\\s+set\\s+(?<key>[^\\s=]+)\\s+)(?<value>${VALUE})`, 'g'),
  // A key=value: API_KEY=K, ?access_token=K, process.env.TOKEN='K',
  // --from-literal=pw=K, :_authToken=K.
  rule(`(?<head>${EDGE}(?<key>${NAME})=)(?<value>${VALUE})`, 'g'),
  // The same with spaces: password = K, pw = "K", export TOKEN = K (not
  // ==, => or =~, and not a bare "key = value", which is prose).
  rule(`(?<head>${EDGE}(?<key>${NAME})(?:[ \\t]+=(?![=>~])[ \\t]*|=[ \\t]+))(?<value>${VALUE})`, 'g', k => isCredentialKey(k) && k.toLowerCase() !== 'key'),
  // A header or a YAML line: Authorization: Bearer K, X-Api-Key: K, and
  // password: K under db: (the value is on the name's line). A bare "auth:"
  // outside a header flag is left alone: it is a commit message's scope far
  // more often.
  rule(`(?<head>(?:^|\\\\[ntr]|[\\s"'])(?<key>${NAME})[ \\t]*:[ \\t]*(?:${SCHEMES}\\s+)?)(?<value>[^\\s"',;\\\\]+)`, 'gi', k => isCredentialKey(k) && k.toLowerCase() !== 'auth'),
]

// Credentials that sit in a command's own syntax rather than after a name.
const arg = (re, ok = () => true) => ({ re, ok })
const IN = (cmd) => `\\b(?:${cmd})\\b[^|;&\\n]*?\\s` // a flag of this command, on its segment
const ARG_RULES = [
  // curl -u user:<value>, --user, -uuser:<value> (not a URL: redis-cli -u
  // redis://… has its own rule), and httpie's -a/--auth user:<value>.
  arg(/(?<head>(?:^|\s)(?:-u|--user|--proxy-user)(?:\s+|=)?(?:\\?["'])?[^\s:"'\\/]*:)(?!\/\/)(?<value>[^\s"'\\]+)/g),
  arg(new RegExp(`(?<head>${IN('https?|httpie')}(?:-a|--auth)(?:${GAP}|=)(?:\\\\?["'])?[^\\s:"'\\\\/]*:)(?<value>[^\\s"'\\\\]+)`, 'g')),
  // mysql -p<value> and 7z -p<value> (attached: with a space, mysql's -p
  // asks for it).
  arg(new RegExp(`(?<head>${IN('mysql|mysqldump|mysqladmin|mariadb|7z|7za|7zr')}-p)(?<value>[^\\s'"\\\\-][^\\s'"\\\\]*)`, 'g')),
  // -p <value> after sshpass, docker/podman login, mongosh and az login;
  // redis-cli -a, ldapsearch -w, zip and unzip -P.
  arg(new RegExp(`(?<head>${IN('sshpass|(?:docker|podman|nerdctl|buildah)\\s+login|mongosh|mongo|mongodump|mongorestore|mongoexport|mongoimport|az\\s+login')}-p(?:${GAP}|=)?)(?!-)(?<value>${VALUE})`, 'g')),
  arg(new RegExp(`(?<head>${IN('redis-cli')}-a(?:${GAP}|=)?)(?!-)(?<value>${VALUE})`, 'g')),
  arg(new RegExp(`(?<head>${IN('ldapsearch|ldapmodify|ldapadd|ldapdelete|ldapwhoami|ldappasswd')}-w(?:${GAP}|=)?)(?!-)(?<value>${VALUE})`, 'g')),
  arg(new RegExp(`(?<head>${IN('zip|unzip|zipcloak')}-P(?:${GAP}|=)?)(?!-)(?<value>${VALUE})`, 'g')),
  // redis-cli AUTH [user] <value>; smbclient -U user%<value>.
  arg(new RegExp(`(?<head>${IN('redis-cli')}AUTH${GAP})(?<value>(?:${VALUE})(?:${GAP}(?:${VALUE}))?)`, 'gi')),
  arg(/(?<head>\bsmbclient\b[^|;&\n]*?\s(?:-U|--user(?:=|\s))\s*["']?[^\s%"']*%)(?<value>[^\s"'\\]+)/g),
  // openssl's pass:<value>.
  arg(/(?<head>(?:^|[\s"'=])pass:)(?<value>[^\s"'\\]+)/g),
  // Every --input of record verify and record replay (the help passes
  // secrets that way); the input's name stays.
  arg(new RegExp(`(?<=\\brecord${GAP}(?:verify|replay)\\b[^|;&\\n]*?)(?<head>--input(?:=|${GAP})(?:[A-Za-z0-9_.-]+=)?)(?<value>${VALUE})`, 'g')),
  // browser type <selector> <text>, when the selector names a credential
  // field (type=password, #pw, [name=otp], .card-number…).
  arg(new RegExp(`(?<head>\\bbrowser${GAP}type(?:${GAP}--?[A-Za-z][\\w-]*)*${GAP}(?<sel>${VALUE})${GAP}(?:--?[A-Za-z][\\w-]*${GAP})*)(?<value>${VALUE})`, 'g'), g => credentialSelector(g.sel)),
]

// The words that make a page field a credential's, read anywhere in its
// selector; the whole words of a key count as words of it.
const FIELD_WORDS = ['password', 'pass', 'pwd', 'pin', 'otp', 'token', 'secret', 'code', 'cvv', 'card', ...CREDENTIAL_WORDS]

// credentialSelector says whether a CSS selector names a credential field.
export function credentialSelector(selector) {
  const sel = String(selector ?? '').replace(/^\\?["']|\\?["']$/g, '')
  if (/type\s*=\s*\\?["']?password/i.test(sel)) return true
  const low = sel.toLowerCase()
  return FIELD_WORDS.some(w => low.includes(w)) || low.split(/[^a-z0-9]+/).some(w => WHOLE.has(w))
}

// Name/value pairs: {"name": "password", "value": K}, an array of them, a
// single-quoted dict, JSON inside a JSON string, a Kubernetes env entry
// (- name: DB_PASSWORD / value: K) and helm's name=password,value=K. When
// the label is a credential's, the value is masked and the label kept.
const OBJECT = /\{[^{}]*\}/g
const PAIR_LABEL = /(?<q>\\?["'])(?:name|key|field|k)\k<q>\s*:\s*(?<q2>\\?["'])(?<label>.*?)\k<q2>/i
const PAIR_VALUE = /(?<q>\\?["'])(?:value|val|v)\k<q>\s*:\s*/gi
const QUOTED_AT = [/^"((?:[^"\\]|\\.)*)/, /^\\"((?:[^"\\]|\\[^"])*)/, /^'([^']*)/, /^([^\s,}\]]+)/]
function maskPairObject(obj) {
  const label = PAIR_LABEL.exec(obj)?.groups.label
  if (label == null || !isCredentialKey(label)) return obj
  let out = ''
  let at = 0
  for (const m of obj.matchAll(PAIR_VALUE)) {
    const start = m.index + m[0].length
    const v = QUOTED_AT.map(re => re.exec(obj.slice(start))).find(Boolean)
    if (start < at || !v || PLAIN.test(v[1]) || v[1] === MASK) continue
    out += obj.slice(at, start + v[0].length - v[1].length) + MASK
    at = start + v[0].length
  }
  return out + obj.slice(at)
}
const PAIR_RULES = [
  /(?<head>\bname:[ \t]*["']?(?<label>[\w.-]+)["']?[ \t]*(?:\r?\n|\\n)[ \t-]*value:[ \t]*)(?<value>"[^"\n]*"?|'[^'\n]*'?|(?:[^\s"'\\]|\\(?!n))+)/gi,
  /(?<head>\b(?:name|key)=(?<label>[^,\s"']+),[ \t]*value=)(?<value>[^,\s"']+)/gi,
]
function maskPairs(text) {
  let out = text.replace(OBJECT, maskPairObject)
  for (const re of PAIR_RULES) {
    out = out.replace(re, (all, ...a) => {
      const { head, label, value } = a.at(-1)
      return isCredentialKey(label) && !PLAIN.test(value) && value !== MASK ? head + MASK : all
    })
  }
  return out
}

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
// the shapes, the values of name/value pairs and after a credential's
// name, and the values in a command's own syntax (ARG_RULES).
export function maskCredentials(text) {
  if (typeof text !== 'string' || !text) return text
  let out = maskPairs(maskSecretShapes(text))
  for (const { re, keyOk } of KEY_RULES) {
    out = out.replace(re, (all, ...a) => {
      const { head, key, value } = a.at(-1)
      const label = LABEL_KEYS.test(key) && inPair(a.at(-2), a.at(-3))
      return keyOk(key) && value !== MASK && !plainValue(key, value) && !label ? head + MASK : all
    })
  }
  for (const { re, ok } of ARG_RULES) {
    out = out.replace(re, (all, ...a) => {
      const g = a.at(-1)
      return g.value === MASK || !ok(g) ? all : g.head + MASK
    })
  }
  return out
}

// maskArgs is a tool call's arguments with every string masked and every
// value under a credential key replaced, at any depth; a name/value pair
// whose label is a credential's ({name: 'password', value: K}) keeps the
// label and masks the value.
export function maskArgs(value, depth = 0) {
  if (typeof value === 'string') return maskCredentials(value)
  if (!value || typeof value !== 'object' || depth > 20) return value
  if (Array.isArray(value)) return value.map(v => maskArgs(v, depth + 1))
  const keys = Object.keys(value)
  const labelKey = keys.find(k => LABEL_KEYS.test(k) && typeof value[k] === 'string')
  const valueKey = keys.find(k => /^(?:value|val|v)$/i.test(k))
  const pair = labelKey && valueKey && isCredentialKey(value[labelKey])
  const hide = (k, v) => v != null && v !== '' && !plainValue(k, String(v)) &&
    (pair ? k === valueKey : isCredentialKey(k) && !(labelKey === k && valueKey))
  return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, hide(k, v) ? MASK : maskArgs(v, depth + 1)]))
}
