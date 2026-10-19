// The #872 review's third round: name/value pairs, the commands that
// take a secret in their own syntax, and what must stay readable. Masking
// is best effort (SECURITY.md). The values are fakes.
import { describe, it, expect } from 'vitest'
import { MASK, credentialSelector, isCredentialKey, maskArgs, maskCredentials } from './credentials.js'

const S = 'Zq8Xfake4417'
const masks = (input, want) => {
  const out = maskCredentials(input)
  expect(out, input).not.toContain(S)
  if (want !== undefined) expect(out).toBe(want)
  expect(maskCredentials(out), input).toBe(out) // masking twice changes nothing
  return out
}

describe('name/value pairs', () => {
  it.each([
    [`{"name":"password","value":"${S}"}`, `{"name":"password","value":"${MASK}"}`],
    [`{"key":"api_key","value":"${S}"}`, `{"key":"api_key","value":"${MASK}"}`],
    [`{"field":"token","value":"${S}"}`, `{"field":"token","value":"${MASK}"}`],
    [`{"Name":"Password","Value":"${S}"}`, `{"Name":"Password","Value":"${MASK}"}`],
    [`{"key":"secret","val":"${S}"}`, `{"key":"secret","val":"${MASK}"}`],
    [`{"k":"token","v":"${S}"}`, `{"k":"token","v":"${MASK}"}`],
    [`{"value":"${S}","name":"DB_PASSWORD"}`, `{"value":"${MASK}","name":"DB_PASSWORD"}`],
    [`{"name":"pin","value":4417}`, `{"name":"pin","value":${MASK}}`],
    [`[{"name":"DB_PASSWORD","value":"${S}"},{"name":"HOST","value":"h"}]`, `[{"name":"DB_PASSWORD","value":"${MASK}"},{"name":"HOST","value":"h"}]`],
    [`{"headers":[{"name":"X-Api-Key","value":"${S}"}]}`, `{"headers":[{"name":"X-Api-Key","value":"${MASK}"}]}`],
    [`{"variablesValues":[{"name":"api_key","value":"${S}"}]}`, `{"variablesValues":[{"name":"api_key","value":"${MASK}"}]}`],
    [`{'name': 'password', 'value': '${S}'}`, `{'name': 'password', 'value': '${MASK}'}`],
    [`monoagentcli workflow run abc --input '[{"name":"password","value":"${S}"}]'`, `monoagentcli workflow run abc --input '[{"name":"password","value":"${MASK}"}]'`],
    [`- name: DB_PASSWORD\n  value: ${S}`, `- name: DB_PASSWORD\n  value: ${MASK}`],
    [`env:\n- name: API_TOKEN\n  value: "${S}"\n- name: HOST\n  value: h`, `env:\n- name: API_TOKEN\n  value: ${MASK}\n- name: HOST\n  value: h`],
    [`helm install x c --set name=password,value=${S}`, `helm install x c --set name=password,value=${MASK}`],
  ])('%s', (input, want) => { masks(input, want) })

  it('masks a pair inside JSON inside a JSON string, and in the raw view', () => {
    masks(JSON.stringify(JSON.stringify({ name: 'password', value: S })))
    masks(JSON.stringify({ command: `kubectl apply -f - <<EOF\n- name: DB_PASSWORD\n  value: ${S}\nEOF` }))
  })

  it('keeps a pair whose label names no credential, and a lone "key" that holds one', () => {
    for (const input of [`{"name":"HOST","value":"${S}"}`, `{"name":"recipientEmail","value":"${S}"}`, `{"key":"region","value":"${S}"}`]) {
      expect(maskCredentials(input)).toBe(input)
    }
    expect(maskCredentials(`{"key":"${S}"}`)).toBe(`{"key":"${MASK}"}`)
  })

  it('masks the value of a pair in tool arguments, keeping its label', () => {
    expect(maskArgs({ variables: [{ name: 'password', value: S }, { name: 'host', value: 'h' }] }))
      .toEqual({ variables: [{ name: 'password', value: MASK }, { name: 'host', value: 'h' }] })
    expect(maskArgs({ key: 'api_key', value: S })).toEqual({ key: 'api_key', value: MASK })
    expect(maskArgs({ Key: 'X-Api-Key', Value: S })).toEqual({ Key: 'X-Api-Key', Value: MASK })
    expect(maskArgs({ key: S })).toEqual({ key: MASK })
  })
})

describe("MonoAgent's commands that take a secret in their own syntax", () => {
  it.each([
    [`monoagentcli record verify rec-1 --input code=${S}`, `monoagentcli record verify rec-1 --input code=${MASK}`],
    [`monoagentcli record replay rec-1 --input otp=${S} --input user=me --yes`, `monoagentcli record replay rec-1 --input otp=${MASK} --input user=${MASK} --yes`],
    [`monoagentcli record verify rec-1 --input="${S}"`, `monoagentcli record verify rec-1 --input=${MASK}`],
    [`monoagentcli browser type "#password" ${S}`, `monoagentcli browser type "#password" ${MASK}`],
    [`monoagentcli browser type "input[name=pw]" "${S}"`, `monoagentcli browser type "input[name=pw]" ${MASK}`],
    [`monoagentcli browser type 'input[type=password]' ${S}`, `monoagentcli browser type 'input[type=password]' ${MASK}`],
    [`monoagentcli browser type --append "#otp-code" ${S}`, `monoagentcli browser type --append "#otp-code" ${MASK}`],
    [`monoagentcli browser type ".card-number" ${S}`, `monoagentcli browser type ".card-number" ${MASK}`],
  ])('%s', (input, want) => { masks(input, want) })

  it('leaves typed text alone in a field that names no credential, and other commands\' --input', () => {
    for (const input of ['monoagentcli browser type "#search" "hello world"', 'monoagentcli browser type "#email" me@x.test',
      'monoagentcli workflow run abc --input city=Paris', 'monoagentcli record replay rec-1 --inputs-file /tmp/in.json']) {
      expect(maskCredentials(input)).toBe(input)
    }
  })

  it('reads a selector as a credential field by type, name or word', () => {
    for (const sel of ['#password', 'input[type="password"]', '#pw', '[name=otp]', '#pin', '.cvv', '#auth-code', '#api_key', '#ssn']) {
      expect(credentialSelector(sel), sel).toBe(true)
    }
    for (const sel of ['#search', '#email', '.title', 'textarea', '#path']) {
      expect(credentialSelector(sel), sel).toBe(false)
    }
  })
})

describe('other tools that take a secret in their own syntax', () => {
  it.each([
    [`redis-cli -a ${S}`, `redis-cli -a ${MASK}`],
    [`redis-cli -h h AUTH ${S}`, `redis-cli -h h AUTH ${MASK}`],
    [`redis-cli AUTH default ${S}`, `redis-cli AUTH ${MASK}`],
    [`mongosh -u a -p ${S}`, `mongosh -u a -p ${MASK}`],
    [`az login -u me -p ${S}`, `az login -u me -p ${MASK}`],
    [`smbclient //h/s -U me%${S}`, `smbclient //h/s -U me%${MASK}`],
    [`ldapsearch -D cn=a -w ${S}`, `ldapsearch -D cn=a -w ${MASK}`],
    [`7z a -p${S} a.7z f`, `7z a -p${MASK} a.7z f`],
    [`zip -P ${S} a.zip f`, `zip -P ${MASK} a.zip f`],
    [`unzip -P ${S} a.zip`, `unzip -P ${MASK} a.zip`],
    [`http -a me:${S} u`, `http -a me:${MASK} u`],
    [`aws configure set aws_secret_access_key ${S}`, `aws configure set aws_secret_access_key ${MASK}`],
  ])('%s', (input, want) => { masks(input, want) })

  it('leaves the same flags alone elsewhere', () => {
    for (const input of ['ls -a', 'redis-cli -h h PING', 'mongosh --port 27017', 'grep -w word f', 'tar -P -cf a.tar f', 'zip -r a.zip d']) {
      expect(maskCredentials(input)).toBe(input)
    }
  })
})

describe('spellings', () => {
  it.each([
    [`password = ${S}`, `password = ${MASK}`],
    [`export TOKEN = ${S}`, `export TOKEN = ${MASK}`],
    [`pw = "${S}"`, `pw = ${MASK}`],
    [`db:\n  password: ${S}\n  host: h`, `db:\n  password: ${MASK}\n  host: h`],
    [`printf 'db:\\n  password: ${S}\\n' > c.yaml`, `printf 'db:\\n  password: ${MASK}\\n' > c.yaml`],
  ])('%s', (input, want) => { masks(input, want) })

  it('leaves comparisons and prose alone', () => {
    for (const input of ['if [ "$a" == "token" ]; then x; fi', 'echo "key = value"', 'x => token', 'a =~ password']) {
      expect(maskCredentials(input)).toBe(input)
    }
  })
})

describe('names that hold a credential word but no credential', () => {
  it.each([
    'python gen.py --max_new_tokens 64', 'curl -d \'{"max_tokens": 100}\' u', 'python tok.py --tokenizer gpt2',
    'npm run build -- --key-file x', 'rsync --password-file=f a b', 'make KEY_BITS=2048', 'docker run -e SESSION_TIMEOUT=30 img',
    'psql -c "select * from t where session_id=5"', 'curl "u?session_id=5"',
  ])('%s', (input) => {
    expect(maskCredentials(input)).toBe(input)
  })

  it('still masks a session id that is not a number', () => {
    masks(`curl "u?session_id=${S}"`, `curl "u?session_id=${MASK}"`)
    expect(isCredentialKey('session_id')).toBe(true)
  })
})
