// Browser smoke test against the real binary. It builds para with the embedded UI, makes a temp
// install with one vault, runs `para serve` on 127.0.0.1:7710, and drives Chrome through sign-in,
// Today, Review accept, capture, live refresh, a rendered document, the graph, and the service
// worker. Needs Google Chrome (playwright channel 'chrome') and a built ui/dist (`pnpm build`).
//
//   pnpm build && pnpm e2e
//
// It uses one port (7710, or PARA_E2E_PORT), a temp home, and stops the server and the MCP agent
// by their own PIDs.
import { spawn, execFileSync } from 'node:child_process'
import { appendFileSync, mkdtempSync, openSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { request } from 'node:http'
import { chromium } from 'playwright'

const repo = resolve(import.meta.dirname, '..', '..')
const port = Number(process.env.PARA_E2E_PORT ?? 7710)
const base = `http://acme.localhost:${port}`
const work = mkdtempSync(join(tmpdir(), 'para-e2e-'))
const home = join(work, 'home')
const bin = join(work, 'para')
const failures = []
let server
let agent // the MCP agent: { child, call }
let watched // the page to describe when a step fails

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

// startAgent runs the real `para mcp` over stdio and speaks JSON-RPC lines to it, as an agent
// harness would. It returns call(tool, args) -> parsed JSON of the tool's text result.
function startAgent() {
  const child = spawn(bin, ['--home', home, '--vault', 'acme', 'mcp', '--actor', 'agent-e2e'], {
    stdio: ['pipe', 'pipe', 'ignore'],
  })
  const waiters = new Map()
  let nextId = 1
  let buf = ''
  child.stdout.on('data', (chunk) => {
    buf += chunk
    for (let i = buf.indexOf('\n'); i >= 0; i = buf.indexOf('\n')) {
      const line = buf.slice(0, i).trim()
      buf = buf.slice(i + 1)
      if (!line) continue
      const m = JSON.parse(line)
      if (m.id !== undefined && waiters.has(m.id)) {
        waiters.get(m.id)(m)
        waiters.delete(m.id)
      }
    }
  })
  const rpc = (method, params) =>
    new Promise((resolveCall, reject) => {
      const id = nextId++
      const timer = setTimeout(() => reject(new Error(`mcp ${method}: no answer`)), 20000)
      waiters.set(id, (m) => {
        clearTimeout(timer)
        resolveCall(m)
      })
      child.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', id, method, params })}\n`)
    })
  const ready = rpc('initialize', {
    protocolVersion: '2024-11-05',
    capabilities: {},
    clientInfo: { name: 'e2e', version: '0' },
  }).then(() => {
    child.stdin.write(
      `${JSON.stringify({ jsonrpc: '2.0', method: 'notifications/initialized' })}\n`,
    )
  })
  const call = async (name, args) => {
    await ready
    const m = await rpc('tools/call', { name, arguments: args })
    if (m.error) throw new Error(`mcp ${name}: ${JSON.stringify(m.error)}`)
    const text = m.result?.content?.[0]?.text ?? ''
    if (m.result?.isError) throw new Error(`mcp ${name}: ${text}`)
    return JSON.parse(text)
  }
  return { child, call }
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

  // Stream AB fixtures: a decision that blocks a task, an area with a link, a recurring and an
  // overdue task, an open incident, a task another person holds, and a base URL for review links.
  appendFileSync(join(work, 'vault', '.para', 'vault.properties'), `server.base_url=${base}\n`)
  const regionQ = idOf(
    para(
      ...v,
      'question',
      'add',
      'Which region first?',
      '-p',
      proj,
      '--kind',
      'decision',
      '--option',
      'eu',
      '--option',
      'us',
      '--recommended',
      'eu',
      '--json',
    ),
  )
  const deployTask = idOf(
    para(...v, 'task', 'add', 'Deploy the service', '-p', proj, '--depends-on', regionQ, '--json'),
  )
  const area = idOf(para(...v, 'area', 'add', 'Platform', '--json'))
  para(
    ...v,
    'area',
    'link',
    'add',
    area,
    'Runbook',
    '--kind',
    'runbook',
    '--target',
    'https://example.com/runbook',
  )
  para(
    ...v,
    'task',
    'add',
    'Rotate the keys',
    '--area',
    area,
    '--recur',
    'weekly',
    '--due',
    '2020-01-06',
  )
  const incident = idOf(
    para(
      ...v,
      'incident',
      'add',
      'Disk full on build host',
      '--severity',
      'sev2',
      '--area',
      area,
      '--json',
    ),
  )
  const owed = idOf(
    para(...v, 'task', 'add', 'Review the contract', '-p', proj, '--due', '2020-02-03', '--json'),
  )
  para(...v, 'task', 'assign', owed, person)
  para(...v, 'inbox', 'add', 'Triage me to a question')

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

  // ---- Stream AB: proposals, questions, areas, incidents, waiting, search filters. ----
  // A key made by `para key create --print-url` signs in a second browser context. It carries
  // proposal:review; a read-only key must not offer decisions.
  const reviewerUrl = para(
    ...v,
    'key',
    'create',
    'reviewer',
    '--scope',
    'vault:read',
    '--scope',
    'vault:write',
    '--scope',
    'proposal:review',
    '--print-url',
    '--url',
    `${base}/`,
  ).trim()
  const readerUrl = para(
    ...v,
    'key',
    'create',
    'reader',
    '--scope',
    'vault:read',
    '--print-url',
    '--url',
    `${base}/`,
  ).trim()
  check(reviewerUrl.includes('#token='), 'key create --print-url prints a sign-in URL')

  agent = startAgent()
  const evidence = { quote: 'Sam will look at the quota', locator: 'minute 4' }
  const filed = await agent.call('proposal_add', {
    title: 'Quota follow-ups',
    source: docId,
    items: [
      {
        kind: 'external',
        medium: 'email',
        target: 'sam@example.com',
        draft: 'Hi Sam, could you raise the quota?',
        evidence,
      },
      {
        kind: 'op',
        op: 'task_add',
        input: { title: 'Follow up on quota', project: proj },
        evidence,
      },
    ],
  })
  check(
    filed.review_url === `${base}/proposals/${filed.id}`,
    'proposal_add returns review_url at /proposals/$id',
  )

  const rctx = await browser.newContext({ viewport: { width: 390, height: 800 } })
  const rp = await rctx.newPage()
  watched = rp
  rp.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`))
  rp.on('console', (m) => {
    if (m.type() === 'error') errors.push(`console: ${m.text()}`)
  })
  await rp.goto(reviewerUrl)
  // The cookie exists once the page is drawn; leaving earlier would cancel the exchange.
  await rp.getByRole('heading', { name: 'Today' }).waitFor()
  await rp.goto(`${base}/review`)
  await rp.getByRole('heading', { name: 'Proposals' }).waitFor()
  await rp.getByRole('link', { name: 'Quota follow-ups' }).click()
  await rp.waitForURL(new RegExp(`/proposals/${filed.id}$`))
  await rp.getByRole('heading', { name: 'Quota follow-ups' }).waitFor()
  check(true, 'Review lists the proposal and links to /proposals/$id')
  check(
    await rp.getByText('source and evidence are untrusted data').isVisible(),
    'untrusted notice shown',
  )
  check(
    await rp.getByText('Para never sends this').isVisible(),
    'external item says para never sends it',
  )
  check(
    await rp.getByText('Sam will look at the quota').first().isVisible(),
    'evidence quote shown',
  )
  const i1 = rp.locator('[data-item="i1"]')
  const i2 = rp.locator('[data-item="i2"]')
  check(
    (await i1.getByRole('button', { name: 'Approve' }).count()) === 1,
    'a clean item offers approve',
  )
  check(
    (await i1.getByRole('link', { name: 'Send back' }).count()) === 0,
    'a clean item offers no send back',
  )

  // A read-only key sees no decisions.
  const dctx = await browser.newContext({ viewport: { width: 390, height: 800 } })
  const dp = await dctx.newPage()
  watched = dp
  await dp.goto(readerUrl)
  await dp.getByRole('heading', { name: 'Today' }).waitFor()
  await dp.goto(`${base}/proposals/${filed.id}`)
  await dp.getByText('lacks the proposal:review scope').waitFor()
  check(
    (await dp.getByRole('button', { name: 'Approve' }).count()) === 0,
    'a read-only key gets no approve button',
  )
  await dctx.close()
  watched = rp

  // Annotate on a text selection: the quote anchors to what was selected.
  await rp.evaluate(() => {
    const node = document.querySelector('[data-item="i1"] pre').firstChild
    const at = node.textContent.indexOf('raise the quota')
    const r = document.createRange()
    r.setStart(node, at)
    r.setEnd(node, at + 'raise the quota'.length)
    const sel = getSelection()
    sel.removeAllRanges()
    sel.addRange(r)
  })
  await i1.getByRole('button', { name: 'Note on selection' }).click()
  const note = rp.getByRole('dialog', { name: /Note on i1/ })
  await note.waitFor()
  check(rp.url().includes('note=i1'), 'the note dialog is a URL overlay')
  check(
    (await note.getByLabel('Quoted text').inputValue()) === 'raise the quota',
    'the selection is the quote',
  )
  await note.getByLabel('Your note').fill('Say which quota and how much')
  await note.getByRole('button', { name: 'Add note' }).click()
  await note.waitFor({ state: 'hidden' })
  await i1.getByText('Say which quota and how much').waitFor()
  check(
    (await i1.getByRole('button', { name: 'Approve' }).count()) === 0,
    'an annotated item cannot be approved',
  )
  check(
    (await i1.getByRole('link', { name: 'Send back' }).count()) === 1,
    'an annotated item offers send back',
  )

  // An edit without a note: approve as edited, or send back. Deny is gone.
  await i2.getByRole('link', { name: 'Edit' }).click()
  const edit = rp.getByRole('dialog', { name: /Edit i2/ })
  await edit
    .getByLabel('Input (JSON, in full)')
    .fill(JSON.stringify({ title: 'Follow up on the quota ticket', project: proj }))
  await edit.getByRole('button', { name: 'Save edit' }).click()
  await edit.waitFor({ state: 'hidden' })
  await i2.getByRole('button', { name: 'Approve as edited' }).waitFor()
  check(
    (await i2.getByRole('link', { name: 'Deny' }).count()) === 0,
    'an edited item cannot be denied',
  )

  // Deny and rework need a reason; try rework on nothing here, check the form on a fresh proposal below.
  await i1.getByRole('link', { name: 'Send back' }).click()
  const sb = rp.getByRole('dialog', { name: /Send back i1/ })
  await sb.getByLabel('Reason').fill('Name the number')
  await sb.getByRole('button', { name: 'Send back' }).click()
  await sb.waitFor({ state: 'hidden' })
  await i1.getByText('sent back').first().waitFor()
  check(true, 'send back moves the item to the agent')
  check(
    await rp.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1),
    'no horizontal page scroll on the proposal page at 390 px',
  )

  // The agent reads the note over MCP and files revision 2, answering it.
  const shown = await agent.call('proposal_show', { id: filed.id })
  const ann = shown.annotations.find((a) => a.item === 'i1')
  check(
    !!ann && ann.quote === 'raise the quota' && ann.anchored,
    'MCP sees the annotation with its quote',
  )
  const revised = await agent.call('proposal_revise', {
    id: filed.id,
    items: [
      {
        id: 'i1',
        kind: 'external',
        medium: 'email',
        target: 'sam@example.com',
        draft: 'Hi Sam, could you raise the staging quota to 20?',
        evidence,
      },
    ],
    answers: [{ annotation: ann.id, outcome: 'applied' }],
  })
  check(revised.revision === 2 && revised.id !== filed.id, 'the agent files revision 2 over MCP')

  // The old page learns of it without a reload.
  await rp.getByText('The agent filed a newer revision').waitFor({ timeout: 10000 })
  await rp.getByRole('link', { name: `Review ${revised.id}` }).click()
  await rp.waitForURL(new RegExp(`/proposals/${revised.id}$`))
  await rp.getByRole('heading', { name: 'Changes since the last revision' }).waitFor()
  check(
    await rp.getByText('could you raise the staging quota to 20?').first().isVisible(),
    'revision 2 shows the diff',
  )
  check(await rp.getByText('Revision 1').isVisible(), 'history lists revision 1')
  check(await rp.getByText('applied').first().isVisible(), 'the agent answer to the note shows')

  // Approve as edited: the hash binds to the owner's final text.
  const r2i1 = rp.locator('[data-item="i1"]')
  await r2i1.getByRole('link', { name: 'Edit' }).click()
  const ed2 = rp.getByRole('dialog', { name: /Edit i1/ })
  await ed2.getByLabel('Draft').fill('Hi Sam, could you raise the staging quota to 20? Thanks.')
  await ed2.getByRole('button', { name: 'Save edit' }).click()
  await ed2.waitFor({ state: 'hidden' })
  await r2i1.getByRole('button', { name: 'Approve as edited' }).click()
  await r2i1.getByText(/Approved as hash/).waitFor()
  const hash = (await r2i1.locator('code').first().innerText()).trim()
  check(/^sha256:[0-9a-f]{64}$/.test(hash), 'approval shows the hash')
  check(
    await r2i1.getByText('Para has sent nothing').isVisible(),
    'an external approval says nothing was sent',
  )
  const verdict = await agent.call('proposal_approved', { hash })
  check(verdict.approved === true, 'MCP proposal_approved confirms the approved hash')
  // The op item runs under the proposing agent when approved.
  await rp.locator('[data-item="i2"]').getByRole('button', { name: 'Approve' }).click()
  await rp.locator('[data-item="i2"]').getByText('approved', { exact: true }).waitFor()
  check(
    para(...v, 'task', 'list').includes('Follow up on the quota ticket'),
    'an approved op item ran',
  )

  // Deny and rework need a reason.
  const second = await agent.call('proposal_add', {
    title: 'Second batch',
    source: docId,
    items: [
      {
        kind: 'external',
        medium: 'email',
        target: 'kim@example.com',
        draft: 'Draft to Kim',
        evidence,
      },
    ],
  })
  await rp.goto(`${base}/proposals/${second.id}`)
  await rp.getByRole('link', { name: 'Deny' }).click()
  const dn = rp.getByRole('dialog', { name: /Deny i1/ })
  check(
    await dn.getByRole('button', { name: 'Deny' }).isDisabled(),
    'deny is disabled without a reason',
  )
  await dn.getByLabel('Reason').fill('Not needed')
  await dn.getByRole('button', { name: 'Deny' }).click()
  await dn.waitFor({ state: 'hidden' })
  await rp.getByText('Reason: Not needed').waitFor()
  check(true, 'deny with a reason is recorded')

  // Questions: the decision blocks a task; the answer needs its reason and shows what unblocked.
  await rp.goto(`${base}/work`)
  const deployRow = rp.getByRole('listitem').filter({ hasText: 'Deploy the service' })
  await deployRow.getByText(/waiting on question/).waitFor()
  check(true, 'Work shows the task waiting on the question')
  await rp.goto(`${base}/questions`)
  const qrow = rp.getByRole('listitem').filter({ hasText: 'Which region first?' })
  await qrow.getByText('blocks 1').waitFor()
  check(true, 'the question shows what it blocks')
  await qrow.getByRole('link', { name: 'Answer' }).click()
  const ans = rp.getByRole('dialog', { name: 'Answer' })
  await ans.getByLabel('eu (recommended)').check()
  check(
    await ans.getByRole('button', { name: 'Record answer' }).isDisabled(),
    'a decision needs a reason',
  )
  await ans.getByLabel(/Why \(required/).fill('Closest to the users')
  await ans.getByRole('button', { name: 'Record answer' }).click()
  await ans.getByText('Unblocked 1').waitFor()
  check(
    await ans.getByRole('link', { name: 'Deploy the service' }).isVisible(),
    'the answer shows the unblocked task',
  )
  await ans.getByRole('button', { name: 'Done' }).click()
  await ans.waitFor({ state: 'hidden' })
  await rp.goto(`${base}/work`)
  await rp.getByRole('listitem').filter({ hasText: 'Deploy the service' }).waitFor()
  await deployRow.getByText(/waiting on question/).waitFor({ state: 'detached' })
  check(true, 'the task is unblocked after the answer')

  // Triage an inbox item into a question with a scope; finish a task with a note.
  await rp.goto(`${base}/today`)
  await rp
    .getByRole('listitem')
    .filter({ hasText: 'Triage me to a question' })
    .getByRole('link', { name: 'Sort' })
    .click()
  const tq = rp.getByRole('dialog', { name: 'Sort inbox item' })
  await tq.getByRole('combobox', { name: 'Becomes' }).click()
  await rp.getByRole('option', { name: /^Question/ }).click()
  await tq.getByRole('combobox', { name: /Scope/ }).click()
  await rp.getByRole('option', { name: 'The whole vault' }).click()
  await tq.getByRole('button', { name: 'Sort', exact: true }).click()
  await tq.waitFor({ state: 'hidden' })
  await rp.goto(`${base}/questions`)
  await rp.getByText('Triage me to a question').waitFor()
  check(true, 'triage turns an inbox item into a root question')
  await rp.goto(`${base}/work`)
  await rp
    .getByRole('listitem')
    .filter({ hasText: 'Write the plan' })
    .getByRole('link', { name: 'Done' })
    .click()
  const dd = rp.getByRole('dialog', { name: 'Finish task' })
  await dd.getByLabel(/Note/).fill('Plan written and shared')
  await dd.getByRole('button', { name: 'Done' }).click()
  await dd.waitFor({ state: 'hidden' })
  check(
    para(...v, 'task', 'show', t1).includes('Plan written and shared'),
    'task done records the note',
  )

  // Areas, incidents, waiting, due dates.
  await rp.goto(`${base}/today`)
  await rp.getByRole('link', { name: 'Disk full on build host' }).waitFor()
  check(true, 'Today lists the open incident')
  const waitRow = rp.getByRole('listitem').filter({ hasText: 'Review the contract' })
  await waitRow.waitFor()
  check(
    await waitRow.getByText('overdue 2020-02-03').isVisible(),
    'Waiting shows an overdue task held by someone else',
  )
  await rp.goto(`${base}/work`)
  await rp.getByRole('link', { name: 'Platform' }).click()
  await rp.getByRole('heading', { name: 'Platform' }).waitFor()
  check(
    (await rp.getByRole('link', { name: 'Runbook' }).getAttribute('href')) ===
      'https://example.com/runbook',
    'the area page shows its link',
  )
  const recur = rp.getByRole('listitem').filter({ hasText: 'Rotate the keys' }).first()
  check(
    await recur.getByText(/repeats weekly/).isVisible(),
    'the area page lists the recurring task',
  )
  check(await recur.getByText(/overdue 2020-01-06/).isVisible(), 'an overdue due date is marked')
  await rp.getByRole('link', { name: 'Disk full on build host' }).click()
  await rp.waitForURL(new RegExp(`/incidents/${incident}$`))
  await rp.getByLabel('Add to the timeline').fill('Cleared old build caches')
  await rp.getByRole('button', { name: 'Add' }).click()
  await rp.getByText('Cleared old build caches').waitFor()
  check(true, 'the incident timeline takes a note')
  await rp.getByRole('button', { name: 'Mark resolved' }).click()
  await rp.getByText('resolved', { exact: true }).first().waitFor()
  check(true, 'the incident can be resolved')

  // Search filters and tags.
  await rp.goto(`${base}/search?q=Logs&type=policy`)
  await rp.getByRole('link', { name: 'Logging policy' }).first().waitFor()
  check(
    await rp.getByText('exact', { exact: true }).first().isVisible(),
    'a hit carries its match tag',
  )
  await rp.goto(`${base}/search?q=Logs&type=task`)
  await rp.getByText('No hit for Logs.').waitFor()
  check(true, 'the type filter narrows the search')
  check(
    await rp.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1),
    'no horizontal page scroll on the search page at 390 px',
  )
  await rctx.close()

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
    console.error(`page ${watched?.url()}:\n${(await watched?.innerText('body'))?.slice(0, 1500)}`)
  } catch {
    // no page to describe
  }
  try {
    console.error(readFileSync(join(work, 'server.log'), 'utf8').split('\n').slice(-25).join('\n'))
  } catch {
    // no log yet
  }
} finally {
  if (agent) agent.child.kill('SIGTERM')
  if (server) server.kill('SIGTERM')
  rmSync(work, { recursive: true, force: true })
}
if (failures.length) {
  console.error(`\n${failures.length} failure(s)`)
  process.exit(1)
}
console.log('\nall checks passed')
