// Credential-shaped values are masked in any command line or tool input a
// card shows (#872 review). The values here are fakes.
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { MASK, isCredentialKey, maskArgs, maskCredentials } from './credentials.js'

describe('the credential keys', () => {
  // The JS rule masks at least what the Go redaction masks.
  it("cover every key of internal/workflow/redact.go's redactKeyPattern", () => {
    const go = readFileSync(join(__dirname, '..', '..', '..', '..', '..', 'internal', 'workflow', 'redact.go'), 'utf8')
    const m = go.match(/redactKeyPattern = regexp\.MustCompile\(\s*`\(\?i\)\^\(([^)]+)\)\$`\)/)
    expect(m, 'redactKeyPattern found').not.toBeNull()
    expect(m[1].split('|').filter(k => !isCredentialKey(k))).toEqual([])
  })

  it('are names that hold a credential word', () => {
    for (const k of ['api_key', 'apiKey', 'API_KEY', 'x-api-key', 'GITHUB_TOKEN', 'accessToken', 'client_secret', 'Authorization',
      'db_password', 'passwd', 'basic_auth', 'X-Auth-Token', 'sessionId', 'Set-Cookie', 'credentials', 'max_tokens']) {
      expect(isCredentialKey(k), k).toBe(true)
    }
    for (const k of ['name', 'title', 'author', 'keyboard', 'monkey', 'oauth_url', 'value', 'field']) {
      expect(isCredentialKey(k), k).toBe(false)
    }
  })
})

// Fakes in each token's shape, built here so no file holds one whole.
const SHAPES = {
  openai: 'sk-' + 'a1B2'.repeat(6),
  anthropic: 'sk-ant-api03-' + 'c3D4'.repeat(6),
  github: 'ghp_' + 'E5f6'.repeat(9),
  githubOAuth: 'gho_' + 'E5f6'.repeat(9),
  githubPat: 'github_pat_' + 'G7h8'.repeat(8),
  slack: 'xoxb-' + '1234-5678-'.repeat(2) + 'abcd',
  aws: 'AKIA' + 'QRST'.repeat(4),
  google: 'AIza' + 'u9V0'.repeat(9),
  jwt: 'eyJ' + 'hbGc'.repeat(4) + '.eyJ' + 'zdWI'.repeat(4) + '.' + 'c2ln'.repeat(4),
}

describe('token shapes', () => {
  it.each(Object.entries(SHAPES))('masks a %s token wherever it stands', (_, token) => {
    expect(maskCredentials(`curl -d ${token} https://x.test`)).toBe(`curl -d ${MASK} https://x.test`)
    expect(maskCredentials(`"note": "use ${token} now"`)).toBe(`"note": "use ${MASK} now"`)
  })

  it('masks what follows Bearer, however short', () => {
    expect(maskCredentials('auth Bearer x1 ok')).toBe(`auth Bearer ${MASK} ok`)
  })
})

describe('maskCredentials', () => {
  it.each([
    // The probe's case, and the camelCase key.
    [`monoagentcli workflow run abc --input '{"api_key":"K"}'`, `monoagentcli workflow run abc --input '{"api_key":"${MASK}"}'`],
    [`monoagentcli workflow run abc --input '{"apiKey":"K","n":2}'`, `monoagentcli workflow run abc --input '{"apiKey":"${MASK}","n":2}'`],
    ['{"api_key": "K", "name": "x"}', `{"api_key": "${MASK}", "name": "x"}`],
    ['{"password": "p\\"q", "n": 1}', `{"password": "${MASK}", "n": 1}`],
    ['{"token": 12345}', `{"token": ${MASK}}`],
    ['{"command":"run --input \'{\\"client_secret\\":\\"K\\"}\'"}', `{"command":"run --input '{\\"client_secret\\":\\"${MASK}\\"}'"}`],
    ["{'private_key': 'K'}", `{'private_key': '${MASK}'}`],
    // Arguments cut short.
    ['{"command":"x","refresh_token":"abc', `{"command":"x","refresh_token":"${MASK}`],
    // Flags.
    ['deploy --token=K --name app', `deploy --token=${MASK} --name app`],
    ['deploy --api-key K --verbose', `deploy --api-key ${MASK} --verbose`],
    ['login --password "a b" --user me', `login --password ${MASK} --user me`],
    // Env and assignments.
    ['API_KEY=K node run.js', `API_KEY=${MASK} node run.js`],
    ['export GITHUB_TOKEN=abc123; make', `export GITHUB_TOKEN=${MASK}; make`],
    ['monoagentcli secret add t --field password=K', `monoagentcli secret add t --field password=${MASK}`],
    ['curl "https://x.test/cb?access_token=K&page=2"', `curl "https://x.test/cb?access_token=${MASK}&page=2"`],
    // Headers.
    ['curl -H "Authorization: Bearer K" https://x.test', `curl -H "Authorization: Bearer ${MASK}" https://x.test`],
    ["curl -H 'X-Api-Key: K' https://x.test", `curl -H 'X-Api-Key: ${MASK}' https://x.test`],
    ['echo Bearer abcdefgh12345678', `echo Bearer ${MASK}`],
  ])('%s', (input, want) => {
    expect(maskCredentials(input)).toBe(want)
    expect(maskCredentials(want)).toBe(want) // masking twice changes nothing
  })

  it.each([
    'go test ./...',
    'monoagentcli task add "Fix the token refresh"',
    '{"title":"Rotate the password","count":5}',
    'git commit --author "Sam <sam@x.test>" -m "Fix the keyboard"',
    'npm run task-runner -- --verbose',
    'monoagentcli secret add --name tok --stdin',
    'git commit -m "auth: tidy"',
  ])('leaves %s as it is', (input) => {
    expect(maskCredentials(input)).toBe(input)
  })

  it('passes what is not text through', () => {
    expect(maskCredentials(undefined)).toBeUndefined()
    expect(maskCredentials('')).toBe('')
  })
})

describe('maskArgs', () => {
  it('masks every string and every value under a credential key, at any depth', () => {
    const args = {
      command: `curl -H "Authorization: Bearer K" x`,
      env: { OPENAI_API_KEY: 'K', HOME: '/home/u' },
      argv: ['--token=K', 'ok'],
      headers: [{ name: 'Cookie', value: 'x' }],
      api_key: 'K',
      timeout: 5,
    }
    expect(maskArgs(args)).toEqual({
      command: `curl -H "Authorization: Bearer ${MASK}" x`,
      env: { OPENAI_API_KEY: MASK, HOME: '/home/u' },
      argv: [`--token=${MASK}`, 'ok'],
      headers: [{ name: 'Cookie', value: 'x' }],
      api_key: MASK,
      timeout: 5,
    })
    expect(args.api_key).toBe('K') // a copy: the call is untouched
  })
})
