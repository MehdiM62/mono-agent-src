// The #872 review's second round, group by group: what masking must catch,
// and what it must leave alone. Masking is best effort; the vault rule
// (toolLinks.test.js) is the guarantee for MonoAgent's own commands. The
// values are fakes, and token-shaped ones are built here so that no file
// holds one whole.
import { describe, it, expect } from 'vitest'
import { MASK, isCredentialKey, maskCredentials, maskSecretShapes } from './credentials.js'

const S = 'Zq8Xfake4417'
const j = (...a) => a.join('')
const masks = (input) => {
  const out = maskCredentials(input)
  expect(out, input).not.toContain(S)
  expect(out, input).toContain(MASK)
  expect(maskCredentials(out), input).toBe(out) // masking twice changes nothing
  return out
}

describe('credential words', () => {
  it('count anywhere in a name', () => {
    for (const k of ['passphrase', 'MONOAGENT_FILE_KEYRING_PASSPHRASE', 'bank_iban', 'card_number', 'cardNumber', 'cvv', 'csrf', 'csrf_token',
      'PHPSESSID', 'licensekey', 'sshkey', 'dsn', 'SENTRY_DSN']) {
      expect(isCredentialKey(k), k).toBe(true)
    }
  })

  it('count as a whole word of a name, never inside another word', () => {
    for (const k of ['pass', 'VAULT_PASS', 'db_pass', 'pwd', 'pw', 'admin_pw', 'pswd', 'otp', 'pin', 'pat', 'sig', 'X-Amz-Signature', 'sid', 'ssn', 'user_ssn']) {
      expect(isCredentialKey(k), k).toBe(true)
    }
    for (const k of ['path', 'spinner', 'spin', 'compass', 'bypass', 'passive', 'pattern', 'signal', 'side', 'classname', 'author', 'keyboard', 'pinned']) {
      expect(isCredentialKey(k), k).toBe(false)
    }
  })

  it.each([
    `gpg --passphrase ${S} -d f.gpg`, `tool passphrase=${S}`, `tool --pwd ${S}`, `tool pass=${S}`, `export VAULT_PASS=${S}`,
    `curl -d 'otp=${S}' u`, `tool --pat ${S}`, `curl 'u?sig=${S}'`, `curl 'u?X-Amz-Signature=${S}'`, `curl -b PHPSESSID=${S} u`,
    `tool iban=${S}`, `curl -d '{"cardNumber":"${S}"}' u`, `tool cvv=${S}`, `tool --dsn ${S}`, `echo '{"pw":"${S}"}'`,
  ])('masks %s', (input) => { masks(input) })
})

// Fakes in each shape, built from parts.
const SHAPES = {
  stripeSecret: j('sk', '_live_', '51Hfakefakefakefake1234abcd'),
  stripeRestricted: j('rk', '_live_', 'fakefakefakefakefake1234'),
  stripeWebhook: j('whsec', '_', 'fakefakefakefakefakefake12'),
  npm: j('npm', '_', 'fakefakefakefakefakefakefakefake1234'),
  gitlab: j('glpat', '-', 'fakefakefakefake1234'),
  sendgrid: j('SG', '.', 'fakefakefakefake12', '.', 'fakefakefakefakefakefakefakefakefakefake1'),
  twilio: j('SK', 'fakefakefakefakefakefakefake1234'),
  shopify: j('shpat', '_', 'fakefakefakefakefakefakefake1234'),
  digitalocean: j('dop', '_v1_', 'fake'.repeat(15), '1234'),
  telegram: j('123456789', ':', 'AAFfakefakefakefakefakefakefakefake12'),
  mailgun: j('key', '-', 'fakefakefakefakefakefakefake1234'),
}

describe('token shapes', () => {
  it.each(Object.entries(SHAPES))('masks a %s token wherever it stands, in input and in output', (_, token) => {
    expect(maskCredentials(`echo ${token} done`)).toBe(`echo ${MASK} done`)
    expect(maskSecretShapes(`got ${token}\n`)).toBe(`got ${MASK}\n`)
  })

  it('masks a Telegram token inside a bot URL', () => {
    expect(maskCredentials(`curl https://api.telegram.test/bot${SHAPES.telegram}/getMe`)).toBe(`curl https://api.telegram.test/bot${MASK}/getMe`)
  })

  it('masks a private key block, PEM or OpenSSH, cut short or whole', () => {
    const begin = j('-----BEGIN ', 'PRIVATE KEY-----')
    const end = j('-----END ', 'PRIVATE KEY-----')
    expect(maskCredentials(`echo "${begin}\nMIIEvQ${S}\n${end}" > k`)).toBe(`echo "${MASK}" > k`)
    expect(maskSecretShapes(`${j('-----BEGIN OPENSSH ', 'PRIVATE KEY-----')}\nb3Blbn${S}`)).toBe(MASK)
  })

  it('masks the secret part of Slack and Discord webhook URLs', () => {
    expect(masks(`curl -X POST https://hooks.slack.com/services/T000/B000/${S}`)).toBe(`curl -X POST https://hooks.slack.com/services/${MASK}`)
    expect(masks(`curl https://discord.com/api/webhooks/123/${S}`)).toBe(`curl https://discord.com/api/webhooks/123/${MASK}`)
  })

  it.each([
    [`git clone https://deploy:${S}@github.com/o/r.git`, `git clone https://deploy:${MASK}@github.com/o/r.git`],
    [`psql "postgres://app:${S}@db.example.test/prod"`, `psql "postgres://app:${MASK}@db.example.test/prod"`],
    [`mongosh "mongodb+srv://app:${S}@cluster0.example.test/db"`, `mongosh "mongodb+srv://app:${MASK}@cluster0.example.test/db"`],
    [`redis-cli -u redis://:${S}@cache.example.test:6379`, `redis-cli -u redis://:${MASK}@cache.example.test:6379`],
    [`celery -b amqp://guest:${S}@mq.example.test//`, `celery -b amqp://guest:${MASK}@mq.example.test//`],
    [`curl ftp://user:${S}@ftp.example.test/f`, `curl ftp://user:${MASK}@ftp.example.test/f`],
    [`DATABASE_URL=postgres://u:${S}@h/db ./run`, `DATABASE_URL=postgres://u:${MASK}@h/db ./run`],
  ])('masks a URL password: %s', (input, want) => {
    expect(masks(input)).toBe(want)
  })

  it('leaves long hex and base64 alone: commit hashes and data look the same', () => {
    for (const input of ['git show a1b2c3d4e5f60718293a4b5c6d7e8f9012345678', 'echo dGhpcy1pcy1hLWxvbmctZmFrZS1zZWNyZXQtdmFsdWUtMDEy']) {
      expect(maskCredentials(input)).toBe(input)
    }
  })
})

describe('credentials in a command\'s own syntax', () => {
  it.each([
    [`curl -u admin:${S} https://x.test`, `curl -u admin:${MASK} https://x.test`],
    [`curl --user admin:${S} https://x.test`, `curl --user admin:${MASK} https://x.test`],
    [`curl -uadmin:${S} https://x.test`, `curl -uadmin:${MASK} https://x.test`],
    [`curl --url smtps://smtp.example.test --user "me:${S}"`, `curl --url smtps://smtp.example.test --user "me:${MASK}"`],
    [`mysql -u root -p${S} db`, `mysql -u root -p${MASK} db`],
    [`sshpass -p ${S} ssh me@host`, `sshpass -p ${MASK} ssh me@host`],
    [`docker login -u me -p ${S}`, `docker login -u me -p ${MASK}`],
    [`gpg --batch --passphrase ${S} -d f.gpg`, `gpg --batch --passphrase ${MASK} -d f.gpg`],
    [`openssl enc -aes-256-cbc -pass pass:${S} -in a`, `openssl enc -aes-256-cbc -pass ${MASK} -in a`],
    [`openssl rsa -passin pass:${S} -in k`, `openssl rsa -passin pass:${MASK} -in k`],
    [`jq --arg pw ${S} '.a=$pw' f`, `jq --arg pw ${MASK} '.a=$pw' f`],
  ])('%s', (input, want) => {
    expect(masks(input)).toBe(want)
  })

  it('leaves -u and -p alone where they are a user or a port', () => {
    for (const input of ['mysql -u root -p db', 'ssh -p 2222 me@host', 'sort -u f.txt', 'docker login -u me --password-stdin']) {
      expect(maskCredentials(input)).toBe(input)
    }
  })
})

describe('how names are written', () => {
  it.each([
    [`node -e "process.env.TOKEN='${S}'"`, `node -e "process.env.TOKEN=${MASK}"`],
    [`curl -d 'auth.token=${S}' u`, `curl -d 'auth.token=${MASK}' u`],
    [`helm install x c --set db.password=${S}`, `helm install x c --set db.password=${MASK}`],
    [`kubectl create secret generic s --from-literal=pw=${S}`, `kubectl create secret generic s --from-literal=pw=${MASK}`],
    [`curl -H 'X-Api-Key:' ${S} u`, `curl -H 'X-Api-Key:' ${MASK} u`],
    [`curl -H "auth: ${S}" u`, `curl -H "auth: ${MASK}" u`],
    [`curl -H "Authorization: Digest username=a, response=${S}" u`, `curl -H "Authorization: ${MASK}" u`],
    [`curl 'u?api%5Fkey=${S}'`, `curl 'u?api%5Fkey=${MASK}'`],
    [`curl 'u?%74oken=${S}'`, `curl 'u?%74oken=${MASK}'`],
    [`tool --api-key \\\n  ${S}`, `tool --api-key \\\n  ${MASK}`],
    [`npm config set //registry.npmjs.org/:_authToken ${S}`, `npm config set //registry.npmjs.org/:_authToken ${MASK}`],
    [`echo "//registry.npmjs.org/:_authToken=${S}" >> .npmrc`, `echo "//registry.npmjs.org/:_authToken=${MASK}" >> .npmrc`],
  ])('%s', (input, want) => {
    expect(masks(input)).toBe(want)
  })

  // The raw view of arguments cut short is their JSON: a newline there is
  // the two characters \n.
  it.each([
    `tool --token\n${S}`,
    `tool --api-key \\\n  ${S}`,
    `cat > .env <<EOF\nAPI_KEY=${S}\nEOF`,
    `cat > .env <<EOF\ndb_pass=${S}\nEOF`,
    `x --token "${S}\nmore"`,
    `printf 'db:\\n  password: ${S}\\n' > c.yaml`,
  ])('masks %j in the raw JSON view too', (command) => {
    masks(command)
    masks(JSON.stringify({ command }))
    masks(JSON.stringify({ command }).slice(0, -5)) // cut short
  })
})

describe('what is left alone', () => {
  it.each([
    'ssh -o StrictHostKeyChecking=no me@host',
    'ssh -o PasswordAuthentication=no -o PubkeyAuthentication=yes me@host',
    '{"token": true, "secret": false, "password": null}',
    'deploy --secret false',
    'API_KEY=yes make',
    'go test ./...',
    'git commit --author="A U Thor <a@x.test>" -m "auth: tidy"',
    'cat path/to/spinner.go',
    'curl -H "Content-Type: application/json" https://x.test',
    'echo "session ended"',
  ])('%s', (input) => {
    expect(maskCredentials(input)).toBe(input)
  })
})

describe('output', () => {
  it('masks token shapes but not every key=value: output is not a command line', () => {
    const out = j('OPENAI_API_KEY=', 'sk-', 'fakefakefakefakefake1234', '\nmax_retries=3\nAPI_KEY=plain\n')
    expect(maskSecretShapes(out)).toBe(`OPENAI_API_KEY=${MASK}\nmax_retries=3\nAPI_KEY=plain\n`)
  })
})
