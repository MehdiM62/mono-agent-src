// Edge cases left over from the #872 review (#892): values with braces, object
// values, quoted and tagged YAML, flags before a key, and token-count names
// that must stay readable. Masking is best effort (SECURITY.md). Fakes only.
import { describe, it, expect } from 'vitest'
import { MASK, isCredentialKey, maskCredentials } from './credentials.js'

const S = 'Zq8Xfake4417'
const masks = (input, want) => {
  const out = maskCredentials(input)
  expect(out, input).not.toContain(S)
  if (want !== undefined) expect(out).toBe(want)
  expect(maskCredentials(out), input).toBe(out)
  return out
}

describe('pairs', () => {
  it('masks a value that contains a closing brace', () => {
    masks(`{"name":"password","value":"a}${S}"}`, `{"name":"password","value":"${MASK}"}`)
  })
  it('masks an object value whole', () => {
    masks(`{"name":"password","value":{"x":"${S}"}}`, `{"name":"password","value":${MASK}}`)
  })
  it('masks an array value whole', () => {
    masks(`{"name":"token","value":["${S}","b"]}`, `{"name":"token","value":${MASK}}`)
  })
  it('finds a pair inside a larger object', () => {
    masks(`{"env":[{"name":"DB_PASSWORD","value":"${S}"},{"name":"HOST","value":"db"}]}`,
      `{"env":[{"name":"DB_PASSWORD","value":"${MASK}"},{"name":"HOST","value":"db"}]}`)
  })
  it('leaves a pair with a plain label alone', () => {
    const t = '{"name":"host","value":"a}b"}'
    expect(maskCredentials(t)).toBe(t)
  })
})

describe('YAML', () => {
  it.each([
    [`password: "${S}"`, `password: ${MASK}`],
    [`password: '${S}'`, `password: ${MASK}`],
    [`db:\n  password: "${S}"\n  host: x`, `db:\n  password: ${MASK}\n  host: x`],
    [`password: &a ${S}`, `password: &a ${MASK}`],
    [`password: !!str ${S}`, `password: !!str ${MASK}`],
    [`password: |\n  ${S}\nhost: x`, `password: ${MASK}\nhost: x`],
    [`password: >-\n  ${S}\n  more\nhost: x`, `password: ${MASK}\nhost: x`],
  ])('%j', (input, want) => { masks(input, want) })
})

describe('commands', () => {
  it('masks aws configure set with flags before the key', () => {
    masks(`aws configure set --profile p aws_secret_access_key ${S}`, `aws configure set --profile p aws_secret_access_key ${MASK}`)
  })
  it('still masks aws configure set without flags', () => {
    masks(`aws configure set aws_secret_access_key ${S}`)
  })
  it('masks browser type into a selector with escaped quotes', () => {
    masks(`browser type "input[type=\\"password\\"]" ${S}`)
  })
  it('shows browser type into a selector that names no credential, by design', () => {
    const t = 'browser type "#field3" hello'
    expect(maskCredentials(t)).toBe(t)
  })
})

describe('token counts and other names that are not credentials', () => {
  it.each(['max-new-tokens', 'max_new_tokens', 'token_budget', 'tokens_per_min', 'tokens-per-min', 'num_tokens', 'n_tokens', 'cache-key', 'sort_key', 'primary-key'])('%s', (k) => {
    expect(isCredentialKey(k)).toBe(false)
    const t = `--${k} 64`
    expect(maskCredentials(t)).toBe(t)
  })
  it.each(['new_token', 'access_tokens', 'GITHUB_TOKEN', 'api-key', 'token', 'key'])('%s is still one', (k) => {
    expect(isCredentialKey(k)).toBe(true)
  })
})
