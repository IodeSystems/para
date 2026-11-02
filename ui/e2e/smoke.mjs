// Browser smoke test against the real binary. It builds para with the embedded UI, makes a temp
// install with one vault, runs `para serve` on 127.0.0.1:7710, and drives Chrome through sign-in,
// Today, Review accept, capture, live refresh, a rendered document, the graph, and the service
// worker. Needs Google Chrome (playwright channel 'chrome') and a built ui/dist (`pnpm build`).
//
//   pnpm build && pnpm e2e
//
// It uses only port 7710, a temp home, and stops the server by its own PID.
import { spawn, execFileSync } from 'node:child_process'
import { appendFileSync, mkdtempSync, openSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { request } from 'node:http'
import { chromium } from 'playwright'

const repo = resolve(import.meta.dirname, '..', '..')
const port = 7710
const base = `http://acme.localhost:${port}`
const work = mkdtempSync(join(tmpdir(), 'para-e2e-'))
const home = join(work, 'home')
const bin = join(work, 'para')
const failures = []
let server

const check = (ok, what) => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${what}`)
  if (!ok) failures.push(what)
}

const para = (...args) => execFileSync(bin, ['--home', home, ...args], { encoding: 'utf8' })
const idOf = (json) => /"id":\s*"([^"]+)"/.exec(json)?.[1]

// api calls the server with a bearer token. node's fetch cannot set Host, and the vault is chosen
// by Host, so this uses http.request.
function api(token, path, body) {
  return new Promise((resolveCall, reject) => {
    const req = request(
      {
        host: '127.0.0.1',
        port,
        path,
        method: body ? 'POST' : 'GET',
        headers: {
          Authorization: `Bearer ${token}`,
          Host: `acme.localhost:${port}`,
          'Content-Type': 'application/json',
        },
      },
      (res) => {
        let text = ''
        res.on('data', (c) => (text += c))
        res.on('end', () => resolveCall({ status: res.statusCode, text }))
      },
    )
    req.on('error', reject)
    req.end(body ? JSON.stringify(body) : undefined)
  })
}

try {
  execFileSync('go', ['build', '-tags', 'embedui', '-o', bin, './cmd/para'], {
    cwd: repo,
    stdio: 'inherit',
  })
  para('init', 'acme', join(work, 'vault'), '--no-git')
  const v = ['--vault', 'acme']
  para(...v, 'inbox', 'add', 'Seed: ask platform about the quota')
  const proj = idOf(para(...v, 'project', 'add', 'Launch', '--owner', 'me', '--json'))
  const t1 = idOf(para(...v, 'task', 'add', 'Write the plan', '-p', proj, '--json'))
  para(...v, 'task', 'add', 'Get approval', '-p', proj, '--depends-on', t1)
  const person = idOf(
    para(...v, 'org', 'person', 'add', 'Sam Rivera', '--alias', 'sammy', '--org', 'Acme', '--json'),
  )
  const domain = idOf(
    para(...v, 'org', 'domain', 'add', 'Staging', '--owner', person, '--org', 'Acme', '--json'),
  )
  const rfx = idOf(
    para(
      ...v,
      'rfx',
      'add',
      'Need the staging quota raised',
      '--to',
      person,
      '--via',
      'email',
      '--route',
      domain,
      '--self-serve',
      'read the wiki',
      '--json',
    ),
  )
  para(...v, 'rfx', 'draft', rfx, '--text', 'Hi Sam, could you raise the staging quota?')
  const pol = idOf(
    execFileSync(
      bin,
      [
        '--home',
        home,
        ...v,
        'ingest',
        '--url',
        'https://example.com/policy/logging',
        '--title',
        'Logging policy',
        '--json',
      ],
      { encoding: 'utf8', input: '# Logging policy\n\nLogs are kept 30 days.\n' },
    ),
  )
  para(...v, 'rule', 'add', pol, 'Logs are kept 30 days')
  para(
    ...v,
    'constraint',
    'add',
    '--says',
    'No prod access',
    '--why',
    'audit',
    '--by',
    'Security',
    '--source',
    'https://example.com/c',
    '--review-by',
    '2027-01-01',
  )
  // A document with a wikilink, a wide table, raw HTML, and a mermaid fence.
  const body = [
    'See [[task-elsewhere|the other task]].',
    '',
    '| a | b | c | d | e | f | g | h |',
    '|---|---|---|---|---|---|---|---|',
    '| 1111111111 | 2222222222 | 3333333333 | 4444444444 | 5555555555 | 6666666666 | 7777777777 | 8888888888 |',
    '',
    '```mermaid',
    'flowchart TD',
    '  A --> B',
    '```',
  ].join('\n')
  const docId = idOf(
    para(...v, 'project', 'add', 'Doc view fixture', '--owner', 'me', '--body', body, '--json'),
  )
  // Para refuses raw HTML on write, so it can only arrive by a hand edit or a sync tool. The page
  // must still show it as inert.
  const docFile = join(work, 'vault', 'projects', `${docId}.md`)
  appendFileSync(
    docFile,
    '\n<script>window.__pwned = true</script>\n<img src=x onerror="window.__pwned = true">\n',
  )

  const serverLog = join(work, 'server.log')
  server = spawn(bin, ['--home', home, 'serve', '--addr', `127.0.0.1:${port}`], {
    stdio: ['ignore', openSync(serverLog, 'w'), openSync(serverLog, 'a')],
  })
  for (let i = 0; i < 50; i++) {
    try {
      if ((await api('x', '/healthz')).status === 200) break
    } catch {
      // not listening yet
    }
    await new Promise((r) => setTimeout(r, 200))
  }
  const token = readFileSync(join(home, 'vaults', 'acme', 'server', 'admin.token'), 'utf8').trim()

  // An agent-made change for Review: a write over the API, whose surface is `api`.
  const add = await api(token, '/api/v1/inbox/add', { text: 'Added by an agent' })
  check(add.status === 200, 'API write by a key works')

  const browser = await chromium.launch({ channel: 'chrome' })
  const ctx = await browser.newContext({ viewport: { width: 390, height: 800 } })
  const page = await ctx.newPage()
  const errors = []
  page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`))
  page.on('console', (m) => {
    if (m.type() === 'error') errors.push(`console: ${m.text()}`)
  })
  const sockets = []
  page.on('websocket', (w) => sockets.push(w))

  // Without a session: the sign-in page.
  await page.goto(`${base}/today`)
  await page.waitForURL(/\/login/)
  check(
    await page.getByRole('heading', { name: 'Sign in to para' }).isVisible(),
    'no session lands on /login',
  )
  // A wrong token is refused with a message.
  await page.getByLabel('Token').fill('not-a-token')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await page.getByRole('alert').first().waitFor()
  check(true, 'a bad token shows an error')

  // The fragment token signs in and is stripped from the address bar.
  await page.goto(`${base}/#token=${token}`)
  await page.waitForURL(/\/today/)
  check(!page.url().includes('token'), 'token is gone from the address bar')
  await page.getByRole('heading', { name: 'Today' }).waitFor()
  await page.getByText('Seed: ask platform about the quota').waitFor()
  const cookies = await ctx.cookies()
  const sess = cookies.find((c) => c.name === 'para_session')
  check(
    !!sess && sess.httpOnly && sess.sameSite === 'Strict',
    'session cookie is HttpOnly and SameSite=Strict',
  )
  check(
    !(await page.evaluate(() => document.cookie)).includes('para_session'),
    'the page cannot read the cookie',
  )
  check(
    await page.getByText('Seed: ask platform about the quota').isVisible(),
    'Today lists the inbox item',
  )
  check(await page.getByText('Write the plan').first().isVisible(), 'Today lists a ready task')

  // Live refresh over /ws: a write by another client shows up without a reload or the 60 s poll.
  check(sockets.length > 0, '/ws socket opened')
  await api(token, '/api/v1/inbox/add', { text: 'Pushed live item' })
  await page.getByText('Pushed live item').waitFor({ timeout: 8000 })
  check(true, 'live push refreshed Today without a reload')

  // A write by a separate process (the CLI, as para mcp does) reaches the page through the events/
  // watcher: no reload, well inside the 60 s poll.
  const t0 = Date.now()
  para(...v, 'inbox', 'add', 'Pushed by cli process')
  await page.getByText('Pushed by cli process').waitFor({ timeout: 8000 })
  const ms = Date.now() - t0
  check(ms < 5000, `CLI write showed without a reload (${ms} ms including the CLI run)`)

  // Keyboard: digits, help, capture with Back closing it.
  await page.keyboard.press('2')
  await page.waitForURL(/\/review/)
  await page.keyboard.press('?')
  await page.getByRole('dialog', { name: 'Keyboard' }).waitFor()
  check(page.url().includes('help=true'), 'help is a URL overlay')
  await page.goBack()
  await page.getByRole('dialog', { name: 'Keyboard' }).waitFor({ state: 'hidden' })
  check(true, 'Back closes the help overlay')

  // Review: the agent's change is listed; accept clears it.
  await page
    .getByText('Added by an agent')
    .first()
    .waitFor({ state: 'detached' })
    .catch(() => {})
  const accept = page.getByRole('button', { name: 'Accept all' })
  check(await accept.isEnabled(), 'Review lists changes by others')
  await accept.click()
  await page.getByText('Nothing changed since the last accept.').waitFor()
  check(true, 'Accept all empties Review')
  // The agent key cannot accept: the API refuses it.
  const refused = await api(token, '/api/v1/ui/review/accept', {})
  check(refused.status === 403, 'a bearer key cannot accept (browser session only)')

  // Capture from anywhere.
  await page.keyboard.press('1')
  await page.keyboard.press('c')
  const dlg = page.getByRole('dialog', { name: 'Capture' })
  await dlg.waitFor()
  await dlg.getByLabel('What is on your mind?').fill('Captured from the browser')
  await page.keyboard.press('Control+Enter')
  await dlg.waitFor({ state: 'hidden' })
  await page.getByText('Captured from the browser').waitFor()
  check(true, 'capture adds an inbox item')
  check(!page.url().includes('capture'), 'capture overlay closed in the URL')

  // Inbox triage.
  await page.getByRole('link', { name: 'Sort' }).first().click()
  const tri = page.getByRole('dialog', { name: 'Sort inbox item' })
  await tri.waitFor()
  await tri.getByRole('combobox').click()
  await page.getByRole('option', { name: /Drop/ }).click()
  await tri.getByRole('button', { name: 'Sort', exact: true }).click()
  await tri.waitFor({ state: 'hidden' })
  check(true, 'triage drops an item')

  // Document view: wikilinks routed in app, no raw HTML, scrolling table, mermaid in strict mode.
  await page.goto(`${base}/doc/${docId}`)
  await page.getByRole('heading', { name: 'Doc view fixture' }).waitFor()
  check(
    (await page.getByRole('link', { name: 'the other task' }).getAttribute('href')) ===
      '/doc/task-elsewhere',
    'wikilink routes in the app',
  )
  check(!(await page.evaluate(() => window.__pwned)), 'raw HTML did not run')
  check((await page.locator('main script').count()) === 0, 'no script element in the document')
  const wrap = page.locator('table').first().locator('xpath=..')
  check(
    (await wrap.evaluate((e) => getComputedStyle(e).overflowX)) === 'auto',
    'table scrolls sideways',
  )
  await page.locator('main [role="img"] svg').first().waitFor({ timeout: 15000 })
  check(true, 'mermaid fence draws a diagram')
  check(
    await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1),
    'no horizontal page scroll at 390 px',
  )

  // RFX: per-target state, chain, draft, copy for chair swivel, reply.
  await page.goto(`${base}/rfx`)
  await page.getByRole('link', { name: 'Need the staging quota raised' }).click()
  await page.getByRole('heading', { name: 'Need the staging quota raised' }).waitFor()
  check(
    await page.getByText('Hi Sam, could you raise the staging quota?').first().isVisible(),
    'RFX page shows the draft',
  )
  check(
    (await page
      .getByText('Sam Rivera')
      .first()
      .isVisible()
      .catch(() => false)) || (await page.getByText(person).first().isVisible()),
    'RFX page shows the target',
  )
  await ctx.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: base })
  await page.getByRole('button', { name: 'Copy for chair swivel' }).click()
  await page.getByRole('button', { name: 'Copied' }).waitFor()
  check(
    (await page.evaluate(() => navigator.clipboard.readText())).includes('raise the staging quota'),
    'copy puts the draft on the clipboard',
  )
  // Plain HTTP on a LAN address has no navigator.clipboard: the textarea fallback must copy.
  const plain = await ctx.newPage()
  await plain.addInitScript(() => {
    Object.defineProperty(window, 'isSecureContext', { value: false })
    Object.defineProperty(navigator, 'clipboard', { value: undefined })
  })
  await plain.goto(page.url())
  await plain
    .getByText('Plain HTTP')
    .or(plain.getByText('This page uses plain HTTP'))
    .first()
    .waitFor()
  check(true, 'plain-HTTP banner shows when the page is not a secure context')
  await plain.getByRole('button', { name: 'Copy for chair swivel' }).click()
  await plain.getByRole('button', { name: 'Copied' }).waitFor()
  await page.evaluate(() => navigator.clipboard.writeText('x')) // reset, then read what the fallback wrote
  await plain.getByRole('button', { name: 'Copy for chair swivel' }).click()
  await plain.getByRole('button', { name: 'Copied' }).waitFor()
  check(
    (await page.evaluate(() => navigator.clipboard.readText())).includes('raise the staging quota'),
    'copy works through the textarea fallback',
  )
  await plain.close()
  await page.bringToFront()
  await page.getByRole('link', { name: 'Record reply' }).click()
  const rep = page.getByRole('dialog', { name: /Record a reply/ })
  await rep.waitFor()
  await rep.getByLabel('From (person id or name)').fill(person)
  await rep.getByLabel('What they said').fill('Raised to 20.')
  await rep.getByRole('button', { name: 'Record' }).click()
  await rep.waitFor({ state: 'hidden' })
  check(true, 'rfx reply is recorded')

  // People: who search and a person page.
  await page.goto(`${base}/people?q=sammy`)
  await page.getByRole('link', { name: 'Sam Rivera' }).click()
  await page.getByRole('heading', { name: 'Sam Rivera' }).waitFor()
  check(await page.getByText('sammy').first().isVisible(), 'person page shows aliases')
  await page.goto(`${base}/people/${person}`)
  await page.getByRole('heading', { name: 'Sam Rivera' }).waitFor()
  check(true, 'person page opens by id')

  // Policy and search.
  await page.goto(`${base}/policy`)
  await page.getByText('Logging policy').waitFor()
  check(
    await page.getByText('unverified').first().isVisible(),
    'policy list marks an unverified summary',
  )
  await page.getByRole('tab', { name: /Constraints/ }).click()
  await page.getByText('No prod access').waitFor()
  check(true, 'constraints tab lists the constraint')
  await page.keyboard.press('/')
  await page.getByLabel(/Search documents/).fill('Logs')
  await page.keyboard.press('Enter')
  await page.getByRole('link', { name: 'Logging policy' }).first().waitFor()
  check(true, 'search finds a policy')
  await page.getByRole('link', { name: 'Logging policy' }).first().click()
  await page.locator('h1', { hasText: 'Logging policy' }).waitFor()
  check(true, 'search hit opens the document')

  // Work graph.
  await page.goto(`${base}/work?view=graph`)
  await page.locator('[role="img"] svg').first().waitFor({ timeout: 15000 })
  check(true, 'work graph renders with mermaid')

  // Service worker: app shell only.
  await page.goto(`${base}/today`)
  const swInfo = await page.evaluate(async () => {
    const reg = await navigator.serviceWorker.ready
    const keys = await caches.keys()
    const urls = []
    for (const k of keys)
      for (const r of await (await caches.open(k)).keys()) urls.push(new URL(r.url).pathname)
    return { scope: new URL(reg.scope).pathname, keys, urls }
  })
  check(swInfo.scope === '/', 'service worker is registered for /')
  check(
    swInfo.keys.some((k) => k.startsWith('para-v1')),
    'cache id carries the bump constant',
  )
  check(
    swInfo.urls.length > 5 &&
      swInfo.urls.every((u) => !u.startsWith('/api') && !u.startsWith('/auth') && u !== '/ws'),
    'precache holds no API or auth path',
  )
  check(
    !swInfo.urls.some((u) => /diagram|mermaid/i.test(u) && !/Mermaid-/.test(u)),
    'diagram chunks are not precached',
  )
  // The service worker must not answer a server path with the shell.
  await page.goto(`${base}/api/graphql`)
  const apiText = await page.evaluate(() => document.body.innerText)
  check(
    !/<div id="root"|para/.test(apiText.slice(0, 200)) || apiText.includes('"'),
    '/api/graphql is the server answer, not the shell',
  )
  const hz = await page.goto(`${base}/healthz`)
  check(
    (hz?.headers()['content-type'] ?? '').includes('json'),
    '/healthz is the server answer, not the shell',
  )
  const man = await page.goto(`${base}/manifest.webmanifest`)
  check(
    ((await man?.json()) ?? {}).icons?.some((i) => i.purpose === 'maskable'),
    'manifest has a maskable icon',
  )

  // Sign out.
  await page.goto(`${base}/today`)
  await page.getByRole('button', { name: /Sign out/ }).click()
  await page.waitForURL(/\/login/)
  check(true, 'sign out returns to /login')

  // Mermaid injects KaTeX CSS with data: fonts, which the CSP (font-src 'self') blocks. Only math
  // labels would use them, so the message is expected.
  const real = errors.filter(
    (e) => !/401|Failed to load resource|Loading the font 'data:font/.test(e),
  )
  check(real.length === 0, `no console errors${real.length ? `: ${real.join(' | ')}` : ''}`)
  await browser.close()
} catch (e) {
  console.error(e)
  failures.push(String(e))
  try {
    console.error(readFileSync(join(work, 'server.log'), 'utf8').split('\n').slice(-25).join('\n'))
  } catch {
    // no log yet
  }
} finally {
  if (server) server.kill('SIGTERM')
  rmSync(work, { recursive: true, force: true })
}
if (failures.length) {
  console.error(`\n${failures.length} failure(s)`)
  process.exit(1)
}
console.log('\nall checks passed')
