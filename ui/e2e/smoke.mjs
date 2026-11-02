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
import { appendFileSync, existsSync, mkdtempSync, openSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { request } from 'node:http'
import { chromium } from 'playwright'

const repo = resolve(import.meta.dirname, '..', '..')
const port = Number(process.env.PARA_E2E_PORT ?? 7710)
const base = `http://acme.localhost:${port}`
// Vault writes fsync. On a busy disk one write takes seconds and the "closes at once" check fails
// for a reason that is not the page, so the run uses tmpfs when there is one, as bin/t does.
// PARA_TEST_DISK=1 keeps it on disk.
const fast = !process.env.PARA_TEST_DISK && existsSync('/dev/shm') ? '/dev/shm' : tmpdir()
const work = mkdtempSync(join(fast, 'para-e2e-'))
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

// more opens the "More" menu of a proposal item and picks an entry; menuCount counts one.
const more = async (card, name) => {
  await card.getByRole('button', { name: 'More decisions for this item' }).click()
  await card
    .page()
    .getByRole('menuitem', { name: new RegExp(`^${name}`) })
    .click()
}
const menuCount = async (card, name) => {
  await card.getByRole('button', { name: 'More decisions for this item' }).click()
  const n = await card
    .page()
    .getByRole('menuitem', { name: new RegExp(`^${name}`) })
    .count()
  await card.page().keyboard.press('Escape')
  await card.page().getByRole('menu').waitFor({ state: 'detached' })
  return n
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
  await page.getByLabel('Access key').fill('not-a-token')
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
  // On a phone the lower sections fold away: Next tasks is behind its button.
  await page.getByRole('button', { name: /^Next tasks/ }).click()
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
  await page.getByRole('dialog', { name: 'Help' }).waitFor()
  check(page.url().includes('help=true'), 'help is a URL overlay')
  await page.goBack()
  await page.getByRole('dialog', { name: 'Help' }).waitFor({ state: 'hidden' })
  check(true, 'Back closes the help overlay')

  // Review: the agent's change is listed; accept clears it.
  await page.getByText('Added by an agent').first().waitFor()
  check(true, 'Review names the item an agent added, not its id')
  const accept = page.getByRole('button', { name: /Mark all \d+ logged changes reviewed/ })
  check(await accept.isEnabled(), 'Review lists changes by others')
  await accept.click()
  await page.getByText('Nothing changed since you last marked changes reviewed.').waitFor()
  check(true, 'Mark all reviewed empties the logged changes')
  // The agent key cannot accept: the API refuses it.
  const refused = await api(token, '/api/v1/ui/review/accept', {})
  check(refused.status === 403, 'a bearer key cannot accept (browser session only)')

  // Capture from anywhere.
  await page.keyboard.press('1')
  await page.keyboard.press('c')
  const dlg = page.getByRole('dialog', { name: 'Capture' })
  await dlg.waitFor()
  await dlg.getByLabel('What is on your mind?').fill('Captured from the browser')
  const closeFrom = Date.now()
  await page.keyboard.press('Control+Enter')
  await dlg.waitFor({ state: 'hidden' })
  const closeMs = Date.now() - closeFrom
  // The dialog must not wait for the pages to refetch.
  check(closeMs < 1000, `capture dialog closes at once (${closeMs} ms)`)
  await page.getByText('Captured from the browser').first().waitFor()
  check(true, 'capture adds an inbox item')
  check(
    await page.getByRole('status').or(page.getByText('Captured to the inbox')).first().isVisible(),
    'the capture toast shows',
  )
  check(!page.url().includes('capture'), 'capture overlay closed in the URL')

  // Inbox triage.
  await page.getByRole('link', { name: 'Sort' }).first().click()
  const tri = page.getByRole('dialog', { name: 'Sort inbox item' })
  await tri.waitFor()
  await tri.getByRole('combobox', { name: 'Turn into' }).click()
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
  await rep.getByLabel('Who replied (name)').fill(person)
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
  // On a phone the four tabs are one select.
  check((await page.getByRole('tab').count()) === 0, 'a phone shows Policy as a select, not tabs')
  await page.getByRole('combobox', { name: 'Show' }).click()
  await page.getByRole('option', { name: /Constraints/ }).click()
  await page.getByText('No prod access').waitFor()
  check(true, 'constraints list the constraint')
  await page.keyboard.press('/')
  await page.getByLabel(/Search documents/).fill('Logs')
  await page.keyboard.press('Enter')
  await page.getByRole('link', { name: 'Logging policy' }).first().waitFor()
  check(true, 'search finds a policy')
  await page.getByRole('link', { name: 'Logging policy' }).first().click()
  await page.locator('h1', { hasText: 'Logging policy' }).waitFor()
  check(true, 'search hit opens the document')

  // Work graph.
  await page.setViewportSize({ width: 1000, height: 800 })
  await page.goto(`${base}/work?view=graph`)
  await page.locator('[role="img"] svg').first().waitFor({ timeout: 15000 })
  check(true, 'work graph renders with mermaid')
  check(await page.getByLabel('Legend').isVisible(), 'the graph has a legend')
  check(
    (await page.locator('[role="img"] svg').first().innerHTML()).includes('Which region first?'),
    'the graph shows a question node with its full text',
  )
  // On a phone the graph is a list: one row per item, with what it waits for.
  await page.setViewportSize({ width: 390, height: 800 })
  await page.reload()
  await page.getByRole('link', { name: 'Get approval' }).waitFor()
  check(
    (await page.locator('[role="img"] svg').count()) === 0,
    'a narrow screen gets a list, not a diagram',
  )
  check(
    await page.getByText(/after Write the plan/).isVisible(),
    'the list says what each item waits for',
  )

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
  await rp.getByRole('link', { name: 'Quota follow-ups' }).first().click()
  await rp.waitForURL(new RegExp(`/proposals/${filed.id}$`))
  await rp.getByRole('heading', { name: 'Quota follow-ups' }).waitFor()
  check(true, 'Review lists the proposal and links to /proposals/$id')
  check(
    await rp.getByText('This text came from a document, not from you.').isVisible(),
    'the first proposal a browser opens explains untrusted text',
  )
  await rp.goto(`${base}/today`)
  await rp.getByText(/Start here - Proposal from/).waitFor()
  check(
    (await rp.getByText(/Needs you:/).count()) === 0,
    'Today puts the proposal first, with no Needs you sentence',
  )
  await rp.getByRole('link', { name: 'Menu' }).click()
  await rp
    .getByRole('navigation')
    .filter({ hasText: 'Questions' })
    .getByText('1 proposal to decide')
    .waitFor()
  check(true, 'the menu marks Review with the proposal count')
  await rp.goto(`${base}/proposals/${filed.id}`)
  await rp.getByRole('heading', { name: 'Quota follow-ups' }).waitFor()
  check(
    (await rp.getByText('untrusted', { exact: true }).count()) >= 1,
    'evidence carries a small untrusted mark',
  )
  check(
    (await rp.getByText('This text came from a document, not from you.').count()) === 0,
    'the untrusted explanation does not return on the next visit',
  )
  check(
    await rp.getByText('Approving marks this OK to send. Para does not send it').isVisible(),
    'a pending email says what approving does',
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
  check((await menuCount(i1, 'Send back')) === 1, 'a clean item offers send back in the menu')
  check((await menuCount(i1, 'Rework')) === 0, 'there is no separate rework')
  check(
    (await i1.getByRole('link', { name: 'Deny' }).count()) === 1,
    'Deny is visible beside Approve',
  )
  check((await rp.getByRole('menu').count()) === 0, 'the menu is closed again')

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
  await more(i1, 'Note')
  const note = rp.getByRole('dialog', { name: /Note on/ })
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
  await more(i2, 'Edit')
  const edit = rp.getByRole('dialog', { name: /^Edit/ })
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
  const sb = rp.getByRole('dialog', { name: /Send back/ })
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
  await rp.getByRole('link', { name: 'Open the newer revision' }).click()
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
  await more(r2i1, 'Edit')
  const ed2 = rp.getByRole('dialog', { name: /^Edit/ })
  await ed2.getByLabel('Draft').fill('Hi Sam, could you raise the staging quota to 20? Thanks.')
  await ed2.getByRole('button', { name: 'Save edit' }).click()
  await ed2.waitFor({ state: 'hidden' })
  await r2i1.getByRole('button', { name: 'Approve as edited' }).click()
  await r2i1.getByText('Approved. Not sent yet.').waitFor()
  check(
    (await r2i1.getByRole('button', { name: 'Copy draft' }).count()) === 1,
    'an approved email offers Copy draft',
  )
  await r2i1.getByText('Details', { exact: true }).click()
  await r2i1.getByText(/Approved as hash/).waitFor()
  const hash = (await r2i1.locator('code').first().innerText()).trim()
  check(/^sha256:[0-9a-f]{64}$/.test(hash), 'approval shows the hash')
  check(
    await r2i1.getByText('Not sent yet').isVisible(),
    'an external approval says it is not sent',
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
  await rp.locator('[data-item="i1"]').getByRole('link', { name: 'Deny' }).click()
  const dn = rp.getByRole('dialog', { name: /^Deny/ })
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
  await ans.getByLabel('eu (recommended)').waitFor()
  check(
    await ans.getByLabel('eu (recommended)').isChecked(),
    'the recommended option is preselected',
  )
  check(
    (await ans.getByLabel('Who told you (optional)').count()) === 0,
    'Who told you is under More',
  )
  check(
    await ans.getByRole('button', { name: 'Record answer' }).isDisabled(),
    'a decision needs a reason',
  )
  await ans.getByLabel(/Reason \(required/).fill('Closest to the users')
  await ans.getByRole('button', { name: 'Record answer' }).click()
  await ans.waitFor({ state: 'hidden' })
  check(
    await rp.getByRole('alert').filter({ hasText: 'Unblocked Deploy the service' }).isVisible(),
    'a toast names what was recorded and the unblocked task, with no modal',
  )
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
  await tq.getByRole('combobox', { name: 'Turn into' }).click()
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
    .first()
    .getByRole('button', { name: /^Actions for/ })
    .click()
  await rp.getByRole('menuitem', { name: /^Done/ }).click()
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
  check(
    (await rp.getByRole('button', { name: /^Done: / }).count()) >= 1,
    'Today task rows have Done in place',
  )
  check(
    (await rp.getByRole('button', { name: /^Change the date of/ }).count()) >= 1,
    'Today task rows have a date action in place',
  )
  const waitRow = rp.getByRole('listitem').filter({ hasText: 'Review the contract' })
  await waitRow.waitFor()
  check(
    await waitRow.getByText('overdue Feb 3, 2020').isVisible(),
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
  check(await recur.getByText(/overdue Jan 6, 2020/).isVisible(), 'an overdue due date is marked')
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
  check((await rp.getByText('exact', { exact: true }).count()) === 0, 'a hit has no exact chip')
  check(
    (await rp.locator('mark').first().innerText()).toLowerCase().startsWith('log'),
    'the matched word is highlighted',
  )
  check(!(await rp.locator('main').innerText()).includes('<<'), 'no raw match markers in a snippet')
  check(await rp.getByText(/\d+ results? for/).isVisible(), 'the result count says results')
  await rp.goto(`${base}/search?q=Logs&type=task`)
  await rp.getByText('No results for \u201cLogs\u201d.').waitFor()
  check(true, 'the type filter narrows the search')
  await rp.getByRole('link', { name: 'Capture this' }).click()
  const seeded = rp.getByRole('dialog', { name: 'Capture' })
  check(
    (await seeded.getByLabel('What is on your mind?').inputValue()) === 'Logs',
    'Capture this starts with the search text',
  )
  await seeded.getByRole('button', { name: 'Cancel' }).click()
  await seeded.waitFor({ state: 'hidden' })
  // Live results while typing, and / returns to the field.
  await rp.goto(`${base}/search`)
  await rp.keyboard.type('retention is', { delay: 20 })
  await rp.keyboard.press('Control+a')
  await rp.keyboard.type('Logs', { delay: 20 })
  await rp.getByRole('link', { name: 'Logging policy' }).first().waitFor({ timeout: 8000 })
  check(rp.url().includes('q=Logs'), 'results follow the typing, without Enter')
  await rp.locator('main h1').click()
  await rp.keyboard.press('/')
  check(
    await rp.evaluate(() => document.activeElement?.id === 'search-input'),
    '/ puts the cursor in the search field',
  )
  check(
    await rp.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1),
    'no horizontal page scroll on the search page at 390 px',
  )

  // Names before ids: no screen shows a raw document id. An id has a type word, a dash, and a slug.
  const rawId =
    /\b(task|project|person|policy|rfx|area|question|proposal|request|incident|constraint|domain)-[a-z0-9]+(-[a-z0-9]+)*/
  for (const path of [
    '/today',
    '/work',
    '/rfx',
    `/rfx/${rfx}`,
    '/review',
    '/policy',
    '/policy?tab=rules',
    '/policy?tab=constraints',
    '/requests',
    '/questions',
    '/people',
    `/people/${person}`,
    `/proposals/${filed.id}`,
    `/search?q=Logs`,
  ]) {
    await rp.goto(`${base}${path}`)
    await rp.locator('main h1').first().waitFor()
    await rp.waitForTimeout(400)
    // A proposal page also prints the untrusted source text as it is: only the items are checked.
    const text = path.startsWith('/proposals/')
      ? (await rp.locator('[data-item]').allInnerTexts()).join('\n')
      : await rp.locator('main').innerText()
    const hit = rawId.exec(text)
    check(!hit, `no raw id on ${path}${hit ? `: ${hit[0]}` : ''}`)
  }
  await rp.goto(`${base}/rfx`)
  await rp
    .getByText(/Sam Rivera, /)
    .first()
    .waitFor()
  check(true, 'an RFX row names the person with the state: Sam Rivera, state')
  await rp.goto(`${base}/policy?tab=rules`)
  await rp.getByText('Logs are kept 30 days').waitFor()
  check(await rp.getByText(/R1 in Logging policy/).isVisible(), 'a rule names its policy')
  await rp.goto(`${base}/doc/${pol}`)
  await rp.getByRole('heading', { name: 'Logging policy' }).waitFor()
  const polText = await rp.locator('main').innerText()
  check(
    !/\^R\d|policy-[a-z]/.test(polText),
    'a policy document shows rules without block ids or policy ids',
  )
  check(
    !/fidelity/i.test(await rp.locator('main').innerText()) ||
      !(await rp.getByText(/Fidelity:/).isVisible()),
    'fidelity sits behind Details',
  )

  // PARA_E2E_SHOTS=<dir> saves a screenshot of each main screen at 390 and 1440 px, to read.
  if (process.env.PARA_E2E_SHOTS) {
    const dir = process.env.PARA_E2E_SHOTS
    const shots = [
      ['today', '/today'],
      ['work', '/work'],
      ['graph', '/work?view=graph'],
      ['rfx', '/rfx'],
      ['review', '/review'],
      ['policy', '/policy'],
      ['requests', '/requests'],
      ['search', '/search?q=Logs'],
      ['doc-table', `/doc/${docId}`],
      ['proposal', `/proposals/${filed.id}`],
    ]
    for (const [w, h] of [
      [390, 844],
      [1440, 900],
    ]) {
      await rp.setViewportSize({ width: w, height: h })
      for (const [name, path] of shots) {
        await rp.goto(`${base}${path}`)
        await rp.locator('main h1').first().waitFor()
        await rp.waitForTimeout(name === 'graph' ? 1500 : 500)
        await rp.screenshot({ path: join(dir, `${name}-${w}.png`), fullPage: true })
      }
    }
    await rp.setViewportSize({ width: 390, height: 800 })
  }

  // Phone: the tabs fold into a menu, and every screen is in it.
  await rp.goto(`${base}/today`)
  await rp.getByRole('link', { name: 'Menu' }).click()
  const nav = rp.getByRole('navigation').filter({ hasText: 'Questions' })
  await nav.getByText('Questions', { exact: true }).waitFor()
  check(rp.url().includes('menu=true'), 'the menu is a URL overlay')
  check((await nav.getByRole('link').count()) === 9, 'the menu lists all nine screens')
  await rp.goBack()
  await nav.waitFor({ state: 'hidden' })
  check(true, 'Back closes the menu')
  check(
    await rp.getByRole('link', { name: /^Capture/ }).isVisible(),
    'the capture button is labeled Capture',
  )
  const quick = rp.getByRole('navigation', { name: 'Quick screens' })
  const quickNames = (await quick.getByRole('link').allInnerTexts()).map((t) =>
    t.replace(/\d+/g, '').replace(/\s+/g, ' ').trim(),
  )
  check(
    quickNames.join('|') === 'Today|Review|Search|Menu',
    `a phone has a bottom bar: Today, Review, Search, Menu (got ${quickNames.join('|')})`,
  )
  await quick.getByRole('link', { name: 'Search' }).click()
  await rp.waitForURL(/\/search/)
  check(true, 'the bottom bar opens Search in one tap')
  await rp.goto(`${base}/today`)
  await rp.getByRole('heading', { name: 'Today' }).waitFor()
  // Help holds the build, the vault, and the key list; the page has no footer stamp.
  await rp.keyboard.press('?')
  const help = rp.getByRole('dialog', { name: 'Help' })
  await help.waitFor()
  check((await help.getByText('Esc', { exact: true }).count()) === 0, 'a phone has no key list')
  check(!(await help.getByText(/Build /).isVisible()), 'the About line is hidden until opened')
  await help.getByText('About', { exact: true }).click()
  check(await help.getByText(/Build /).isVisible(), 'the build hash is in the help overlay')
  await rp.keyboard.press('Escape')
  await help.waitFor({ state: 'hidden' })
  check(
    !(await rp.locator('body').innerText()).includes('ui '),
    'no build stamp in the page footer',
  )

  // A wide table scrolls, says so, and right-aligns numbers, on a phone too.
  await rp.goto(`${base}/doc/${docId}`)
  await rp.locator('td').first().waitFor()
  check(
    (await rp
      .locator('td')
      .first()
      .evaluate((e) => getComputedStyle(e).display)) === 'block',
    'a wide table is cards below 600 px',
  )
  await rp.setViewportSize({ width: 700, height: 800 })
  await rp.getByText('Scroll sideways for more columns.').waitFor()
  check(true, 'a wide table says it scrolls sideways')
  check(
    (await rp
      .locator('td')
      .first()
      .evaluate((e) => getComputedStyle(e).textAlign)) === 'right',
    'numbers in a table are right-aligned',
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
  await page.getByText('You are signed out.').waitFor()
  check(true, 'sign out says you are signed out')
  check(
    await page.getByText('para key create <name> --print-url', { exact: true }).isVisible(),
    'sign-in shows the CLI hint as code',
  )
  check(!(await page.locator('body').innerText()).includes('`'), 'sign-in has no literal backticks')

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
  console.error(`\n${failures.length} failure(s):`)
  for (const f of failures) console.error(`  FAILED: ${f}`)
  process.exit(1)
}
console.log('\nall checks passed')
