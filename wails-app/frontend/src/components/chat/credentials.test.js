// Credential-shaped values are masked in any command line or tool input a
// card shows (#872 review). The values here are fakes.
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { CREDENTIAL_KEYS, MASK, isCredentialKey, maskArgs, maskCredentials } from './credentials.js'

describe('the credential keys', () => {
  // One list on each side: the JS list is the Go redaction's.
  it("are internal/workflow/redact.go's redactKeyPattern", () => {
    const go = readFileSync(join(__dirname, '..', '..', '..', '..', '..', 'internal', 'workflow', 'redact.go'), 'utf8')
    const m = go.match(/redactKeyPattern = regexp\.MustCompile\(\s*`\(\?i\)\^\(([^)]+)\)\$`\)/)
    expect(m, 'redactKeyPattern found').not.toBeNull()
    expect([...CREDENTIAL_KEYS].sort()).toEqual(m[1].split('|').sort())
  })

  it('match as written, with - for _, and as a suffix', () => {
    for (const k of ['api_key', 'API_KEY', 'api-key', 'GITHUB_TOKEN', 'x-api-key', 'client_secret', 'Authorization', 'db_password']) {
      expect(isCredentialKey(k), k).toBe(true)
    }
    for (const k of ['tokens', 'max_tokens', 'name', 'title', 'token_file', 'oauth', 'value', 'field']) {
      expect(isCredentialKey(k), k).toBe(false)
    }
  })
})

describe('maskCredentials', () => {
  it.each([
    // The probe's case.
    [`monoagentcli workflow run abc --input '{"api_key":"K"}'`, `monoagentcli workflow run abc --input '{"api_key":"${MASK}"}'`],
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
    '{"title":"Rotate the password","max_tokens":5}',
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
