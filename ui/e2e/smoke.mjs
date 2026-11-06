// Smoke test against the real binary. Agents write through MCP; the owner only reviews in the web
// app. It builds para with the embedded UI (or uses PARA_E2E_BIN), makes a temp install with one
// synthetic vault, seeds it with the CLI, the MCP server, and one closed dossier file, runs
// `para serve` on 127.0.0.1, signs in Chrome with a read-only key, and checks that every main page
// loads, shows no write control, and logs no console error; that /stack lands on /next; and that a
// closed dossier is read only on /doc/<id>. Then it signs in with a review:decide key and checks
// receipts, Next, IO drafts, notes, the composer, and Asked of agents. Needs Google Chrome
// (playwright channel 'chrome') and a built ui/dist (`pnpm build`) when it builds the binary.
//
//   pnpm build && PARA_E2E_PORT=8245 pnpm e2e
//
// It uses one port (7710, or PARA_E2E_PORT), a temp home, and stops the server and the MCP agent
// by their own PIDs.
import { spawn, execFileSync } from 'node:child_process'
import {
  existsSync,
  mkdtempSync,
  mkdirSync,
  openSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { request } from 'node:http'
import { chromium } from 'playwright'

const repo = resolve(import.meta.dirname, '..', '..')
const port = Number(process.env.PARA_E2E_PORT ?? 7710)
const base = `http://acme.localhost:${port}`
const fast = !process.env.PARA_TEST_DISK && existsSync('/dev/shm') ? '/dev/shm' : tmpdir()
const work = mkdtempSync(join(fast, 'para-e2e-'))
const home = join(work, 'home')
const bin = process.env.PARA_E2E_BIN ? resolve(process.env.PARA_E2E_BIN) : join(work, 'para')
const failures = []
let server
let agent
let watched

const check = (ok, what) => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${what}`)
  if (!ok) failures.push(what)
}

const v = ['--vault', 'acme']
const para = (...args) => execFileSync(bin, ['--home', home, ...args], { encoding: 'utf8' })
const idOf = (json) => /"id":\s*"([^"]+)"/.exec(json)?.[1]
// paraIn runs para with text on stdin, as an agent passes a document it read.
const paraIn = (input, ...args) =>
  execFileSync(bin, ['--home', home, ...args], { encoding: 'utf8', input })

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
// harness would. call(tool, args) returns the parsed JSON of the tool's text result.
function startAgent() {
  const child = spawn(bin, ['--home', home, ...v, 'mcp', '--actor', 'agent-e2e'], {
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

// WRITE_WORDS name controls that would change the vault. The viewer has none of them.
const WRITE_WORDS =
  /^(add|add to inbox|new agent work|edit|edit question|save|resolve|done|start|block|later|not later|answer|record (answer|reply)|request|ask (agent|someone)|investigate|delegate|decide|approve|deny|hold|accept|decline|respond|send|dismiss|fix|verified|wrong|revert|undo|undo last mark|mark .*|reopen|schedule|reschedule|cancel|sort|take|drop|cut a dependency|set|waits on|to question|capture)\b/i

// FILTER_WORDS label the text fields that only choose what a page shows.
const FILTER_WORDS =
  /who|show|search|filter|find|days|format|type|status|goal|area|since|updated|by|person|window/i

// writeControls lists the visible buttons, menu items, and text fields that look like writes.
// Navigation, copy, help, theme, sign out, filters, and the search box are allowed.
async function writeControls(page) {
  const found = []
  for (const role of ['button', 'menuitem', 'link']) {
    for (const el of await page.getByRole(role).all()) {
      if (!(await el.isVisible())) continue
      const name = ((await el.getAttribute('aria-label')) || (await el.innerText()) || '').trim()
      // A link to a document may carry any title; only a link that opens a form counts.
      if (role === 'link') {
        const href = (await el.getAttribute('href')) ?? ''
        if (
          /[?&](capture|answer|done|resolve|later|triage|reply|request|docedit|noteedit)=/.test(
            href,
          )
        )
          found.push(`${role} ${name} (${href})`)
        continue
      }
      if (WRITE_WORDS.test(name)) found.push(`${role} ${name}`)
    }
  }
  for (const el of await page.locator('textarea, input[type="text"], input:not([type])').all()) {
    if (!(await el.isVisible()) || (await el.getAttribute('aria-hidden')) === 'true') continue
    const label = await el.evaluate(
      (e) => e.labels?.[0]?.textContent || e.getAttribute('aria-label') || e.id,
    )
    // Search boxes and filters choose what to show; they write nothing.
    if (FILTER_WORDS.test(label ?? '')) continue
    found.push(`input ${label}`)
  }
  return found
}

try {
  if (!process.env.PARA_E2E_BIN) {
    execFileSync('go', ['build', '-tags', 'embedui', '-o', bin, './cmd/para'], {
      cwd: repo,
      stdio: 'inherit',
    })
  }
  // Seed a synthetic vault with the CLI.
  para('init', 'acme', join(work, 'vault'), '--no-git')
  const proj = idOf(
    para(...v, 'project', 'add', 'Launch', '--owner', 'me', '--done-when', 'Launched.', '--json'),
  )
  const task = idOf(para(...v, 'task', 'add', 'Write the plan', '-p', proj, '--json'))
  // A new vault is at format 10: an incident is a task of kind incident.
  const incident = idOf(
    para(...v, 'incident', 'add', 'Cache outage', '--severity', 'sev2', '--json'),
  )
  const question = idOf(
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
      '--json',
    ),
  )
  const person = idOf(para(...v, 'org', 'person', 'add', 'Sam Rivera', '--org', 'Acme', '--json'))
  const domain = idOf(
    para(...v, 'org', 'domain', 'add', 'Staging', '--owner', person, '--org', 'Acme', '--json'),
  )
  const ask = idOf(
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
      '--json',
    ),
  )
  const policy = idOf(
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
  // The policy is an area that follows its source; an exception is an annotation in its body,
  // due for review in a week.
  const reviewBy = new Date(Date.now() + 7 * 24 * 3600 * 1000).toISOString().slice(0, 10)
  para(
    ...v,
    'annotate',
    policy,
    'Thirty days from write or from rotate?',
    '--kind',
    'exception',
    '--field',
    'kind=underspecified',
    '--field',
    'by=Sam Rivera',
    '--field',
    `review_by=${reviewBy}`,
  )
  const meeting = idOf(
    para(...v, 'meeting', 'add', 'Quota sync', '--with', person, '--rfx', ask, '--json'),
  )
  // A commitment: an obligation owed to a person, due tomorrow.
  const owedDue = new Date(Date.now() + 24 * 3600 * 1000).toISOString().slice(0, 10)
  const owed = idOf(
    para(
      ...v,
      'obligation',
      'add',
      'Send Sam the numbers',
      '--owed-to',
      person,
      '--due',
      owedDue,
      '--json',
    ),
  )
  const incoming = idOf(
    para(
      ...v,
      'io',
      'add',
      '--direction',
      'in',
      '--from',
      person,
      '--medium',
      'email',
      '--ref',
      'https://mail.example/m/quota-1',
      'Seed: ask platform about the quota',
      '--json',
    ),
  )
  const outgoing = idOf(
    para(
      ...v,
      'io',
      'add',
      '--direction',
      'out',
      '--to',
      person,
      '--medium',
      'email',
      '--expects',
      'none',
      '--subject',
      'Thanks for the quota',
      'Thanks, Sam.',
      '--json',
    ),
  )

  // A tracked document: the original, a changed version, and a comment on the changed line.
  const tracked = idOf(
    paraIn(
      '# Portal runbook\n\nStep one: drain the node.\nStep two: restart the pods.\n',
      ...v,
      'doc',
      'track',
      '--url',
      'https://wiki.acme.example/runbooks/portal',
      '--system',
      'confluence',
      '--project',
      proj,
      '--json',
    ),
  )
  paraIn(
    '# Portal runbook\n\nStep one: drain the node.\nStep two: restart the pods one at a time.\n',
    ...v,
    'doc',
    'version',
    'add',
    tracked,
  )
  para(
    ...v,
    'doc',
    'comment',
    'add',
    tracked,
    '--by',
    person,
    '--text',
    'Which pods first?',
    '--quote',
    'restart the pods one at a time',
  )

  // Sources: one an agent reaches through its own MCP connector, one no agent declared.
  para(...v, 'source', 'add', '--title', 'Work mailbox', '--example', 'gmail')
  para(...v, 'source', 'add', '--title', 'Code reviews', '--example', 'github')

  // Agent writes go through MCP, as they do for real: each leaves a receipt.
  agent = startAgent()
  await agent.call('agent_sources', { sources: ['work-mailbox'] })
  const note = await agent.call('note_add', {
    title: 'Quota numbers',
    body: 'Staging uses 80 percent of its quota.',
    project: proj,
    why: 'Sam asked for the numbers in one place.',
    sources: ['https://example.com/quota-dashboard'],
    verify: 'Open the note.',
  })
  check(!!note.id, 'MCP seeds a note')

  // A closed dossier from before dossiers were retired: a file in proposals/, read only. It is
  // written straight into the vault, since nothing makes a dossier now.
  const closedDossier = 'proposal-e2e-closed'
  const closedTldr = 'Pick vendor B, it costs less.'
  mkdirSync(join(work, 'vault', 'proposals'), { recursive: true })
  writeFileSync(
    join(work, 'vault', 'proposals', `${closedDossier}.md`),
    [
      '---',
      'type: proposal',
      'created: 2026-11-01',
      'status: superseded',
      'revision: 1',
      'proposed_by: agent',
      '---',
      '# Vendor pick',
      '',
      '````yaml para-dossier',
      `tldr: ${closedTldr}`,
      '````',
      '',
      '## Findings',
      '',
      'Two vendors answered.',
      '',
      '```yaml para-message id=m1',
      'people: [ada@acme.example]',
      'body: Hi Ada.',
      'evidence: {quote: "B costs less", locator: "https://acme.example/l"}',
      'state: moved',
      `result: ${task}`,
      '```',
      '',
    ].join('\n'),
  )
  para(...v, 'reconcile')
  check(para(...v, 'lint').includes('0 errors'), 'para lint accepts the closed dossier')

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

  // A read-only key, as `para serve login` mints.
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
  const readToken = decodeURIComponent(/#token=([^&\s]+)/.exec(readerUrl)?.[1] ?? '')
  check(readToken !== '', 'key create prints a sign-in URL')

  // The read-only key may not write, by GraphQL or by REST.
  const gqlWrite = await api(readToken, '/api/graphql', {
    query:
      'mutation { para { v1 { ioAdd(body: { direction: in, text: "should be refused" }) { id } } } }',
  })
  // The gateway answers 200 with the op's 403 in errors; the data holds no new id.
  {
    let body = {}
    try {
      body = JSON.parse(gqlWrite.text)
    } catch {
      // not JSON: judged by the status alone
    }
    const refused =
      gqlWrite.status === 403 ||
      ((body.errors ?? []).length > 0 &&
        /forbidden|403|scope/i.test(JSON.stringify(body.errors)) &&
        !body.data?.para?.v1?.ioAdd?.id)
    check(
      refused,
      `a GraphQL mutation with a vault:read key is refused (${gqlWrite.status}: ${gqlWrite.text.slice(0, 160)})`,
    )
  }
  check(
    !para(...v, 'io', 'list', '--all').includes('should be refused'),
    'the refused write left no item',
  )

  const browser = await chromium.launch({ channel: 'chrome' })
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const page = await ctx.newPage()
  watched = page
  const errors = []
  page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`))
  page.on('console', (m) => {
    if (m.type() === 'error') errors.push(`console ${page.url()}: ${m.text()}`)
  })
  const writes = []
  page.on('request', (r) => {
    if (r.method() !== 'POST' || !r.url().includes('/api/graphql')) return
    const body = r.postData() ?? ''
    // The viewer sends only reads and the vault:read ops that mint a live cap or log a copy.
    if (/^\s*\{?"?query"?\s*:\s*"\s*mutation/.test(body) || /"query":"mutation/.test(body)) {
      if (!/mutation (LiveSubscribe|CopyDoc|CalendarExport)\b/.test(body))
        writes.push(body.slice(0, 120))
    }
  })

  await page.goto(readerUrl)
  await page.waitForURL(/\/agenda/)
  await page.getByRole('heading', { name: 'Agenda' }).waitFor()
  check(!page.url().includes('token'), 'the read-only key signs in and leaves the address bar')
  check(
    (await page.locator('[data-banner="read-only"]').count()) === 1,
    'a read-only key shows the read-only banner',
  )

  const pages = [
    ['/next', 'Next'],
    ['/agenda', 'Agenda'],
    ['/agenda?by=date', 'Agenda'],
    ['/work', 'Work'],
    ['/rfx', 'Asks'],
    [`/rfx/${ask}`, 'Need the staging quota raised'],
    ['/people', 'People'],
    [`/people/${person}`, 'Sam Rivera'],
    ['/policy', 'Areas'],
    ['/areas', 'Areas'],
    [`/areas/${policy}`, 'Logging policy'],
    ['/questions', 'Questions'],
    ['/requests', 'Asked of agents'],
    ['/obligations', 'Obligations'],
    [`/obligations/${meeting}`, 'Quota sync'],
    [`/obligations/${owed}`, 'Send Sam the numbers'],
    ['/meetings', 'Obligations'],
    [`/meetings/${meeting}`, 'Quota sync'],
    ['/activity', 'Agent activity'],
    ['/friction', 'Friction'],
    ['/health', 'Health'],
    ['/io', 'IO'],
    ['/io?view=outbox', 'IO'],
    [`/io/${incoming}`, 'Seed: ask platform about the quota'],
    [`/io/${outgoing}`, 'Thanks for the quota'],
    ['/docs', 'Docs'],
    [`/docs/${tracked}`, 'Portal runbook'],
    ['/sources', 'Sources'],
    ['/sources/work-mailbox', 'Work mailbox'],
    [`/doc/${closedDossier}`, 'Vendor pick'],
    [`/projects/${proj}`, 'Launch'],
    [`/doc/${task}`, 'Write the plan'],
    [`/incidents/${incident}`, 'Cache outage'],
    [`/doc/${question}`, 'Which region first?'],
    [`/doc/${note.id}`, 'Quota numbers'],
    [`/doc/${policy}`, 'Logging policy'],
    ['/search?q=quota', 'Search'],
  ]
  for (const [path, heading] of pages) {
    const before = errors.length
    await page.goto(`${base}${path}`)
    await page.getByRole('heading', { name: heading, exact: false }).first().waitFor()
    // Let the page's queries settle before reading it.
    await page.waitForLoadState('networkidle').catch(() => {})
    const bad = await writeControls(page)
    check(bad.length === 0, `${path}: no write controls${bad.length ? `: ${bad.join(' | ')}` : ''}`)
    const errs = errors.slice(before).filter((e) => !/Loading the font 'data:font/.test(e))
    check(
      errs.length === 0,
      `${path}: no console errors${errs.length ? `: ${errs.join(' | ')}` : ''}`,
    )
  }

  // IO: the inbox lists what came, the outbox what is ready or drafted, the agenda counts them.
  await page.goto(`${base}/io`)
  await page.getByRole('heading', { name: 'IO' }).waitFor()
  await page.getByRole('link', { name: 'Seed: ask platform about the quota' }).waitFor()
  check(true, 'the IO inbox lists the incoming message')
  await page.getByRole('tab', { name: /Outbox/ }).click()
  await page.getByRole('link', { name: 'Thanks for the quota' }).waitFor()
  check(
    (await page.getByText('Expects no response', { exact: false }).count()) === 1,
    'the outbox groups drafts by what they expect',
  )
  await page.goto(`${base}/agenda`)
  await page.getByRole('heading', { name: 'Agenda' }).waitFor()
  check(
    await page
      .getByRole('link', { name: /1 unread in the inbox, 0 ready to send/ })
      .waitFor({ timeout: 5000 })
      .then(() => true)
      .catch(() => false),
    'the agenda counts unread and ready messages',
  )

  // An incident is a task of kind incident: /incidents/<id> lands on its document page, which shows
  // the severity and the status.
  await page.goto(`${base}/incidents/${incident}`)
  await page.waitForURL((u) => u.pathname === `/doc/${incident}`)
  await page.getByRole('heading', { name: 'Cache outage' }).first().waitFor()
  check(
    await page
      .locator(`[data-incident="${incident}"]`)
      .getByText('sev2')
      .waitFor({ timeout: 5000 })
      .then(() => true)
      .catch(() => false),
    '/incidents/<id> redirects to the document page, which shows the incident',
  )

  // Policy is part of areas: /policy lands on /areas, which lists the exception due for review;
  // the policy's area page shows the exception inline as a callout card.
  await page.goto(`${base}/policy`)
  await page.waitForURL(/\/areas$/)
  await page.getByRole('heading', { name: 'Areas' }).first().waitFor()
  check(
    await page
      .locator('[data-expiring]')
      .getByText('Thirty days from write or from rotate?', { exact: false })
      .waitFor({ timeout: 5000 })
      .then(() => true)
      .catch(() => false),
    '/policy redirects to /areas, which lists the exception due for review',
  )
  await page.goto(`${base}/areas/${policy}`)
  await page.getByRole('heading', { name: 'Logging policy' }).first().waitFor()
  const callout = page.locator('aside[data-kind="exception"]')
  check(
    await callout
      .getByText('Thirty days from write or from rotate?')
      .waitFor({ timeout: 5000 })
      .then(() => true)
      .catch(() => false),
    'the area page shows the exception as a callout',
  )
  check(
    (await callout.getByText(`review by ${reviewBy}`).count()) === 1 &&
      (await page.locator('[data-follows]').count()) === 1,
    'the callout shows its review date, and the page the source it follows',
  )

  // Sources: who reaches each, and which no agent reaches.
  await page.goto(`${base}/sources`)
  await page.getByRole('heading', { name: 'Sources' }).waitFor()
  await page.locator('[data-source="code-reviews"]').waitFor()
  check(
    (await page
      .locator('[data-source="code-reviews"]')
      .getByText('No agent reaches it')
      .count()) === 1,
    'the sources page marks a source no agent reaches',
  )
  check(
    (await page
      .locator('[data-source="work-mailbox"]')
      .getByText('reached by agent-e2e', { exact: false })
      .count()) === 1,
    'the sources page names the agent that declared a source',
  )
  await page.goto(`${base}/sources/work-mailbox`)
  await page.getByRole('heading', { name: 'Work mailbox' }).waitFor()
  await page.locator('[data-actor="agent-e2e"]').waitFor()
  check(
    (await page.locator('[data-actor="agent-e2e"]').count()) === 1 &&
      (await page.locator('[data-step="Scan in"]').count()) === 1,
    'the source page shows the declaration and the steps',
  )

  // Docs: the change since nothing was seen, the comment's quote marked in the text.
  await page.goto(`${base}/docs/${tracked}`)
  await page.getByRole('heading', { name: 'Portal runbook' }).waitFor()
  await page.getByText('+ Step two: restart the pods one at a time.').waitFor()
  check(true, 'the doc page shows what changed')
  check(
    (await page.locator('mark[data-comment="c1"]').innerText()) ===
      'restart the pods one at a time',
    "the comment's quote is marked in the text",
  )
  check(
    (await page.locator('[data-comment-id="c1"]').count()) === 1,
    'the doc page lists the comment',
  )
  check(
    (await page.getByRole('button', { name: 'Mark seen' }).count()) === 0,
    'with a read-only key there is no Mark seen',
  )

  // Keys: c opens nothing; ? opens help.
  await page.goto(`${base}/agenda`)
  await page.getByRole('heading', { name: 'Agenda' }).waitFor()
  await page.keyboard.press('c')
  check((await page.getByRole('dialog').count()) === 0, 'c opens no capture form')
  await page.keyboard.press('?')
  await page.getByRole('dialog').waitFor()
  check(
    (await page
      .getByRole('dialog')
      .getByText('Agents write through the chat', { exact: false })
      .count()) === 1,
    'help says where agents write',
  )
  await page.keyboard.press('Escape')

  // The review stack is retired: /stack lands on /next, and so does an old stack link with a filter.
  await page.goto(`${base}/stack`)
  await page.waitForURL((u) => u.pathname === '/next')
  check(new URL(page.url()).pathname === '/next', `/stack lands on /next (${page.url()})`)
  await page.goto(`${base}/stack?state=closed`)
  await page.waitForURL((u) => u.pathname === '/next')
  check(true, '/stack with a filter lands on /next')

  // A closed dossier is a read-only document: /proposals/<id> lands on /doc/<id>, shows the tldr,
  // and has no write control.
  await page.goto(`${base}/proposals/${closedDossier}`)
  await page.waitForURL((u) => u.pathname === `/doc/${closedDossier}`)
  check(
    new URL(page.url()).pathname === `/doc/${closedDossier}`,
    `/proposals/<id> lands on /doc/<id> (${page.url()})`,
  )
  await page.getByRole('heading', { name: 'Vendor pick' }).first().waitFor()
  await page.getByText(closedTldr, { exact: false }).first().waitFor()
  check(true, 'the closed dossier page shows the tldr')
  check(
    (await page.getByText('Two vendors answered.').count()) === 1,
    'the closed dossier page shows its body',
  )
  check(
    (await page.getByRole('link', { name: 'Open the proposal' }).count()) === 0 &&
      (await page
        .getByRole('button', { name: /^(approve|decline|reject|submit|skip|done|send to agent)/i })
        .count()) === 0,
    'the closed dossier page has no write button',
  )
  {
    const bad = await writeControls(page)
    check(
      bad.length === 0,
      `the closed dossier page has no write controls${bad.length ? `: ${bad.join(' | ')}` : ''}`,
    )
  }

  // Sign out works and lands on the sign-in page.
  await page.getByRole('button', { name: 'Sign out' }).click()
  await page.waitForURL(/\/login/)
  check(true, 'sign out returns to /login')

  check(
    writes.length === 0,
    `the read-only browser sent no write${writes.length ? `: ${writes.join(' | ')}` : ''}`,
  )

  // The owner: a key that may decide.
  const reviewUrl = para(
    ...v,
    'key',
    'create',
    'reviewer',
    '--scope',
    'vault:read',
    '--scope',
    'review:decide',
    '--print-url',
    '--url',
    `${base}/`,
  ).trim()
  const rctx = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const rpage = await rctx.newPage()
  watched = rpage
  const rerrors = []
  rpage.on('pageerror', (e) => rerrors.push(`pageerror: ${e.message}`))
  rpage.on('console', (m) => {
    if (m.type() === 'error' && !/Loading the font 'data:font/.test(m.text()))
      rerrors.push(`console ${rpage.url()}: ${m.text()}`)
  })
  await rpage.goto(reviewUrl)
  // The owner (review:decide) lands on Next.
  await rpage.waitForURL(/\/next/)
  // Mark seen: the owner read the change; the new-versions chip goes.
  await rpage.goto(`${base}/docs/${tracked}`)
  await rpage.getByRole('heading', { name: 'Portal runbook' }).waitFor()
  await rpage.getByRole('button', { name: 'Mark seen' }).click()
  await rpage.getByRole('button', { name: 'Mark seen' }).waitFor({ state: 'detached' })
  check(
    (await rpage.getByText('2 new versions').count()) === 0 &&
      para(...v, 'doc', 'tracked', '--json').includes('"last_seen": 2'),
    'Mark seen marks the document read to the latest version',
  )

  // Receipts: the owner checks or undoes agent writes. The confirm is in the row, never a dialog.
  const rcDialogs = []
  rpage.on('dialog', (d) => {
    rcDialogs.push(d.message())
    void d.dismiss()
  })
  const revertMe = await agent.call('note_add', {
    title: 'E2E revert me',
    body: 'A note the owner undoes.',
    project: proj,
    why: 'Seeded to test undo.',
    sources: [note.id, 'https://example.com/receipts-undo'],
    verify: 'Open the note.',
  })
  const verifyMe = await agent.call('note_add', {
    title: 'E2E verify me',
    body: 'A note the owner verifies.',
    project: proj,
    why: 'Seeded to test the check.',
    verify: 'Open the note.',
  })
  // A write's receipt is found by its document.
  const receiptOf = async (doc) => (await agent.call('receipt_list', { doc })).items[0]?.id
  const revertRid = await receiptOf(revertMe.id)
  const verifyRid = await receiptOf(verifyMe.id)
  const quotaRid = await receiptOf(note.id)
  check(!!revertRid && !!verifyRid && !!quotaRid, 'MCP seeds two more notes with receipts')
  await rpage.goto(`${base}/receipts`)
  await rpage.getByRole('heading', { name: 'Receipts', exact: true }).waitFor()
  const rcRows = rpage.locator('[data-receipt]')
  await rcRows.first().waitFor()
  const rcRow = (id, state) =>
    rpage.locator(`[data-receipt="${id}"]${state ? `[${state}]` : ''}`).first()
  const rcRevert = rcRow(revertRid)
  const rcVerify = rcRow(verifyRid)
  const rcOld = rcRow(quotaRid)
  await rcRevert.waitFor()
  const rcOrder = await rcRows.evaluateAll((els) => els.map((e) => e.getAttribute('data-receipt')))
  const rcAt = (id) => rcOrder.indexOf(id)
  check(
    rcAt(verifyRid) >= 0 && rcAt(verifyRid) < rcAt(revertRid) && rcAt(revertRid) < rcAt(quotaRid),
    'the receipts page lists the newest write first',
  )
  check(
    (await rcRevert.getByText('Seeded to test undo.').count()) === 1 &&
      (await rcRevert.getByRole('link', { name: 'https://example.com/receipts-undo' }).count()) ===
        1 &&
      (await rcRevert.locator('[data-sources] a[href*="/doc/"]').count()) >= 1,
    'a receipt row shows why, an address source as a link, and a vault source as a document link',
  )
  check((await rcOld.getAttribute('data-check')) === 'unchecked', 'a new receipt is unchecked')

  // Filter by check state.
  await rpage.getByLabel('Actor').fill('nobody-e2e')
  await rpage.getByText('No receipt matches.').waitFor()
  check((await rcRows.count()) === 0, 'the actor filter drops other actors')
  await rpage.getByLabel('Actor').fill('')
  await rcRevert.waitFor()

  // Undo: the row asks, then reverts. No browser dialog.
  await rcRevert.getByRole('button', { name: 'Undo', exact: true }).click()
  const confirmUndo = rcRevert.getByRole('button', { name: 'Confirm undo' })
  await confirmUndo.waitFor()
  check(
    (await rcRevert.getByRole('button', { name: 'Cancel' }).count()) === 1 &&
      rcDialogs.length === 0,
    'Undo asks in the row with Confirm undo and Cancel, and no dialog opens',
  )
  await confirmUndo.click()
  await rpage.locator(`[data-outcomes] [data-undo="${revertRid}"]`).waitFor()
  await rcRow(revertRid, 'data-undone="true"').waitFor()
  check(
    (await rcRevert.getAttribute('data-undone')) === 'true',
    'after the confirm the row is marked undone',
  )
  check(
    (await rpage.locator('[data-outcomes] [data-undo]').count()) >= 1,
    'the undo shows in the Outcomes section',
  )
  check(rcDialogs.length === 0, 'the receipts page opened no browser dialog')

  // Mark verified.
  await rcVerify.getByRole('button', { name: 'Mark verified' }).click()
  await rcRow(verifyRid, 'data-check="verified"').waitFor()
  check(
    (await rcVerify.locator('[data-check-chip="verified"]').count()) === 1,
    'Mark verified shows a verified chip',
  )

  // Mark wrong needs a note.
  await rcOld.getByRole('button', { name: 'Mark wrong' }).click()
  const saveWrong = rcOld.getByRole('button', { name: 'Save wrong' })
  check(await saveWrong.isDisabled(), 'Mark wrong needs a note before it saves')
  await rcOld.getByLabel('What is wrong').fill('The quota is 70 percent.')
  await saveWrong.click()
  await rcRow(quotaRid, 'data-check="wrong"').waitFor()
  check(
    (await rcOld.getByText('The quota is 70 percent.').count()) === 1,
    'the wrong mark shows its note',
  )
  check(
    rerrors.length === 0,
    `the receipts page logs no console errors${rerrors.length ? `: ${rerrors.join(' | ')}` : ''}`,
  )
  // The shared card frame: decline, skip, ask agent, edit a message, done by me, a path body.
  const outDraft = (subject, text) =>
    idOf(
      para(
        ...v,
        'io',
        'add',
        '--direction',
        'out',
        '--to',
        person,
        '--medium',
        'email',
        '--expects',
        'none',
        '--subject',
        subject,
        text,
        '--json',
      ),
    )
  const ioPage = outDraft('Minutes', 'Hi Sam, the minutes.')
  const ioApprove = outDraft('Slides', 'Hi Sam, the slides are attached.')
  // Done by me on an outbox draft page.
  await rpage.goto(`${base}/io/${ioPage}`)
  await rpage.getByRole('heading', { name: 'Minutes' }).waitFor()
  await rpage.locator('[data-act="done"]').click()
  await rpage.getByLabel('Where it is, with no link (optional)').fill('sent from my phone')
  await rpage.getByRole('button', { name: 'Confirm done' }).click()
  await rpage.getByText('Marked sent by you.').waitFor()
  check(
    (await agent.call('io_show', { id: ioPage })).item.state === 'sent',
    'Done by me on an outbox draft marks it sent',
  )

  // PU-PREP: an outgoing draft with text is ready; the agenda marks it, and the chip opens the
  // draft. Approve on its page approves its exact text (io_approve, the owner's path).
  await rpage.goto(`${base}/agenda`)
  await rpage.locator(`[data-ready="${ioApprove}"]`).first().waitFor()
  check(
    /Ready: draft reply/.test(
      (await rpage.locator(`[data-ready="${ioApprove}"]`).first().textContent()) ?? '',
    ),
    'the agenda marks a prepared draft Ready',
  )
  await rpage.goto(`${base}/io/${ioApprove}`)
  await rpage.getByRole('heading', { name: 'Slides' }).waitFor()
  await rpage.locator('[data-act="approve"]').click()
  await rpage.locator('[data-approved]').waitFor()
  {
    const ok = (await agent.call('io_show', { id: ioApprove })).item
    check(ok.state === 'ready', `Approve on an outgoing draft makes it ready (${ok.state})`)
  }

  // PU-NEXT: one card, the top of the value order that needs you. Skip (j) shows the next card and
  // writes nothing; k brings the skipped card back. Done leaves with a toast whose Undo brings the
  // card back. Approve on a ready draft approves it and the card leaves.
  {
    const ioNext = outDraft('Badge photo', 'Hi Sam, here is the badge photo.')
    const deck = (await agent.call('order', {})).rows.filter(
      (r) => r.needs_you && r.bucket !== 'not_yet',
    )
    const cardOf = (id) => rpage.locator(`[data-next-card="${id}"]`)
    await rpage.goto(`${base}/next`)
    await rpage.locator('[data-next-card]').waitFor()
    check(
      (await rpage.locator('[data-next-card]').getAttribute('data-next-card')) === deck[0].id,
      `/next shows the top of the order (${deck[0].id})`,
    )
    check(
      /needs? you/.test((await rpage.locator('[data-next-header]').textContent()) ?? ''),
      'the Next header counts what needs you',
    )
    const writes = []
    const onWrite = (r) => {
      const body = r.postData() ?? ''
      if (r.method() === 'POST' && /"query":"\s*mutation (?!LiveSubscribe)/.test(body))
        writes.push(body.slice(0, 80))
    }
    rpage.on('request', onWrite)
    await rpage.keyboard.press('j')
    await cardOf(deck[1].id).waitFor()
    await rpage.keyboard.press('k')
    await cardOf(deck[0].id).waitFor()
    await rpage.locator('[data-next-card] [data-act="skip"]').click()
    await cardOf(deck[1].id).waitFor()
    rpage.off('request', onWrite)
    check(
      writes.length === 0,
      `Skip shows the next card and sends no write (${writes.join(' | ')})`,
    )
    // Skip on to the first task, then Done.
    await rpage.goto(`${base}/next`)
    await cardOf(deck[0].id).waitFor()
    const ti = deck.findIndex((r) => r.kind === 'task')
    for (let i = 0; i < ti; i++) {
      await rpage.keyboard.press('j')
      await cardOf(deck[i + 1].id).waitFor()
    }
    const t = deck[ti]
    await cardOf(t.id).locator('[data-act="done"]').click()
    await cardOf(t.id).waitFor({ state: 'detached' })
    check(
      /"status":\s*"done"/.test(para(...v, 'task', 'show', t.id, '--json')),
      `Done on the Next card closes the task (${t.id})`,
    )
    const toast = rpage.locator('[data-toast="info"]').filter({ hasText: t.title }).last()
    await toast.locator('[data-act="undo"]').click()
    await cardOf(t.id).waitFor()
    check(
      /"status":\s*"todo"/.test(para(...v, 'task', 'show', t.id, '--json')),
      'Undo of Done reopens the task and its card comes back',
    )
    // Approve on a ready draft.
    const di = deck.findIndex((r) => r.id === ioNext)
    check(di >= 0 && deck[di].ready_kind === 'draft', `the new draft is in the deck, ready (${di})`)
    await rpage.goto(`${base}/next`)
    await cardOf(deck[0].id).waitFor()
    for (let i = 0; i < di; i++) {
      await rpage.keyboard.press('j')
      await cardOf(deck[i + 1].id).waitFor()
    }
    check(
      /badge photo/.test((await cardOf(ioNext).locator('[data-next-draft]').textContent()) ?? ''),
      'a ready draft shows its text on the card',
    )
    await rpage.keyboard.press('a')
    await cardOf(ioNext).waitFor({ state: 'detached' })
    const ok = (await agent.call('io_show', { id: ioNext })).item
    check(ok.state === 'ready', `a on a ready draft approves it and the card leaves (${ok.state})`)
  }

  // Notes on any item: a save on notes that changed since they were opened is refused, the page
  // shows the current notes, and the next save, based on them, goes through.
  const noted = idOf(para(...v, 'task', 'add', 'Book the venue', '-p', proj, '--json'))
  await rpage.goto(`${base}/doc/${noted}`)
  await rpage.getByRole('heading', { name: 'Book the venue' }).waitFor()
  await rpage.locator('[data-notes-act="edit"]').click()
  para(...v, 'notes', 'edit', noted, '--md', 'Agent: the venue holds 40.')
  await rpage.getByLabel('Notes (markdown)').fill('Owner: book the hall for 30.')
  await rpage.locator('[data-notes-act="save"]').click()
  await rpage.locator('[data-notes-stale]').waitFor()
  check(
    (await rpage.locator('[data-notes-stale]').innerText()).includes('the venue holds 40'),
    'a notes save on a stale hash is refused and shows the current notes',
  )
  check(
    JSON.parse(para(...v, 'notes', 'read', noted, '--json')).md === 'Agent: the venue holds 40.',
    'the refused save changed nothing',
  )
  await rpage.locator('[data-notes-act="save"]').click()
  await rpage.locator('[data-notes-act="edit"]').waitFor()
  check(
    JSON.parse(para(...v, 'notes', 'read', noted, '--json')).md === 'Owner: book the hall for 30.',
    'the save based on the current notes goes through',
  )

  // An obligation owed tomorrow is an agenda row; its page names who it is owed to.
  await rpage.goto(`${base}/agenda?by=date`)
  await rpage.locator(`[data-agenda-row="${owed}"]`).first().waitFor()
  check(true, 'the agenda lists an owed obligation as a row')
  await rpage.goto(`${base}/obligations/${owed}`)
  await rpage.locator('[data-owed-to]').waitFor()
  check(
    (await rpage.locator('[data-owed-to]').innerText()).includes('Sam Rivera'),
    'the obligation page names who it is owed to',
  )
  check(
    /"type":\s*"obligation"/.test(para(...v, 'show', owed, '--json')),
    'an owed obligation is type obligation',
  )

  // New recurring artifact: the owner makes an obligation an agent owes from the web form; its
  // page shows the owner, the template, and that nothing is produced yet.
  await rpage.goto(`${base}/obligations`)
  await rpage.locator('[data-new-recurring]').click()
  await rpage.locator('[data-field="title"]').fill('Weekly hall report')
  await rpage.locator('[data-field="agent"]').fill('writer')
  await rpage.locator('[data-field="template"]').fill('Bookings, money in, repairs.')
  await rpage.locator('[data-recurring-submit]').click()
  await rpage.waitForURL(/\/obligations\/obligation-[^/]+$/)
  const recurring = rpage.url().split('/').pop()
  await rpage.locator('[data-produce-agent]').waitFor()
  check(
    (await rpage.locator('[data-produce-agent]').innerText()) === 'agent:writer' &&
      (await rpage.locator('[data-produce-template]').innerText()).includes('Bookings, money in') &&
      (await rpage.locator('[data-produce-state]').innerText()).includes('nothing produced yet'),
    'the recurring artifact page shows the agent, the template, and its state',
  )
  check(
    /"produce":\s*\{[^}]*"kind":\s*"report"/.test(para(...v, 'show', recurring, '--json')),
    'the form made an obligation with a produce block',
  )

  // A meeting that needs you (booked for tomorrow, within due_soon_days) shows its brief on the
  // agenda without a click, with an attendee not in the directory as unresolved.
  const soon = new Date(Date.now() + 26 * 3600 * 1000)
  soon.setUTCMinutes(0, 0, 0)
  const soonEnd = new Date(soon.getTime() + 30 * 60 * 1000)
  const briefed = idOf(
    para(
      ...v,
      'meeting',
      'add',
      'Brief sync',
      '--with',
      person,
      '--attendee',
      'Pat Nobody <pat@elsewhere.example>',
      '--start',
      soon.toISOString().replace('.000Z', 'Z'),
      '--end',
      soonEnd.toISOString().replace('.000Z', 'Z'),
      '--json',
    ),
  )
  await rpage.goto(`${base}/agenda`)
  const briefRow = rpage.locator(`[data-agenda-row="${briefed}"] [data-brief="${briefed}"]`)
  await briefRow.waitFor()
  check(
    (await briefRow.locator('[data-brief-unresolved]').count()) === 1,
    'the agenda opens the brief of a meeting that needs you, with the unknown attendee unresolved',
  )

  // A meeting's page shows its brief (the person and the ask it should settle). With notes saved,
  // Send to agent to file queues agent work of kind meeting-notes.
  await rpage.goto(`${base}/meetings/${meeting}`)
  await rpage.waitForURL(new RegExp(`/obligations/${meeting}$`))
  await rpage.locator(`[data-brief="${meeting}"] [data-brief-line="${ask}"]`).first().waitFor()
  check(
    (await rpage.locator(`[data-brief-person="${person}"]`).count()) === 1,
    'the meeting brief lists the person it is with',
  )
  para(...v, 'notes', 'edit', meeting, '--md', 'Decided: the quota stays at 10.')
  await rpage.reload()
  await rpage.locator('[data-act="file-notes"]:not([disabled])').click()
  const notesSent = rpage.locator('[data-file-notes-sent]')
  await notesSent.waitFor()
  const notesReq = (await notesSent.getAttribute('data-file-notes-sent')) ?? ''
  check(
    /"kind":\s*"meeting-notes"/.test(para(...v, 'request', 'show', notesReq, '--json')),
    'Send to agent to file queues meeting-notes agent work',
  )
  await rpage.goto(`${base}/doc/${noted}`)
  await rpage.getByRole('heading', { name: 'Book the venue' }).waitFor()

  // The composer at the foot of the page: a file and words go to an agent as composer work, with a
  // toast and Undo; Save as notes adds them below the notes there.
  const composer = rpage.locator(`[data-item-composer="${noted}"]`)
  await composer.getByLabel(/Tell the agent about/).fill('Booked the hall for 30.')
  await composer.locator('[data-composer-input]').setInputFiles({
    name: 'venue quote.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('Synthetic quote: 30 seats.'),
  })
  await composer.locator('[data-composer-file]').waitFor()
  await composer.locator('[data-composer-send]').click()
  const sent = rpage
    .locator('[data-toast="info"]')
    .filter({ hasText: 'Sent to agent: Book the venue' })
  await sent.waitFor()
  {
    const work = (await agent.call('request_list', {})).items.find(
      (r) => r.title === 'From the owner: Book the venue',
    )
    const shown = work ? await agent.call('request_show', { id: work.id }) : null
    const body = shown?.doc?.body ?? ''
    check(
      !!work &&
        work.status === 'queued' &&
        body.includes('Booked the hall for 30.') &&
        body.includes(`](attachments/${noted}/`),
      'Send to agent queues composer work with the words and the attachment embed',
    )
    const path = /\]\((attachments\/[^)]+)\)/.exec(body)?.[1] ?? ''
    check(
      para(...v, 'attachment', 'read', path).includes('Synthetic quote: 30 seats.'),
      'the attached file is in the vault under attachments/<id>/',
    )
    await sent.getByRole('button', { name: 'Undo' }).click()
    await rpage.locator('[data-toast]').filter({ hasText: 'Undone' }).first().waitFor()
    const after = await agent.call('request_show', { id: work?.id ?? '' })
    check(after.doc?.meta?.status === 'cancelled', 'Undo takes the composer work back')
  }
  // An owner-ask the agent records from the chat shows under Asked of agents, claimed by it. The
  // old Asked of agents tab (/stack?view=sent) lands on /requests.
  {
    const ask = await agent.call('request_add', {
      kind: 'owner-ask',
      title: 'Check the backups',
      ask: 'Check that last night backups ran.',
      owner_said: 'can you check the backups ran last night?',
    })
    check(
      ask.status === 'claimed',
      `an owner-ask the agent records is claimed by it (${ask.status})`,
    )
    await rpage.goto(`${base}/stack?view=sent`)
    const row = rpage.locator(`[data-sent="${ask.id}"][data-kind="owner-ask"]`)
    await row.waitFor()
    const text = await row.innerText()
    check(
      text.includes('Check the backups') &&
        text.includes('You asked: can you check the backups ran last night?') &&
        text.includes('has it'),
      'Asked of agents lists the owner-ask with the owner words and who has it',
    )
    check(
      new URL(rpage.url()).pathname === '/requests',
      `the old tab lands on /requests (${rpage.url()})`,
    )
    await agent.call('request_done', { id: ask.id, resolution: 'done', result: 'Backups ran.' })
    // A done result the owner has not seen needs them; Mark seen clears it.
    await rpage.goto(`${base}/requests`)
    const done = rpage.locator(`[data-status-group="done"] [data-sent="${ask.id}"][data-seen="no"]`)
    await done.waitFor()
    check(
      (await done.innerText()).includes('result to check'),
      'a done owner-ask shows as a result to check',
    )
    await rpage.locator('[data-mark-seen]').click()
    await rpage.locator(`[data-sent="${ask.id}"][data-seen="yes"]`).waitFor()
    const seen = await agent.call('request_show', { id: ask.id })
    check(!!seen.doc?.meta?.seen, 'Mark seen sets seen on the request')
    await rpage.goto(`${base}/doc/${noted}`)
  }
  await composer.getByLabel(/Tell the agent about/).fill('Deposit paid.')
  // A 1x1 PNG: the notes show it inline from the attachment route.
  await composer.locator('[data-composer-input]').setInputFiles({
    name: 'receipt.png',
    mimeType: 'image/png',
    buffer: Buffer.from(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
      'base64',
    ),
  })
  await composer.locator('[data-composer-file]').waitFor()
  await composer.locator('[data-composer-more]').click()
  await rpage.locator('[data-composer-action="Save as notes"]').click()
  await rpage
    .locator('[data-toast="info"]')
    .filter({ hasText: 'Saved as notes: Book the venue' })
    .waitFor()
  {
    const md = JSON.parse(para(...v, 'notes', 'read', noted, '--json')).md
    check(
      md.startsWith('Owner: book the hall for 30.') &&
        md.includes('\n\nDeposit paid.\n\n![Image attachment receipt]'),
      'Save as notes adds a dated entry below the notes there',
    )
    const img = rpage.locator(`[data-notes="${noted}"] [data-attachment] img`)
    await img.waitFor()
    await rpage.waitForFunction((id) => {
      const el = document.querySelector(`[data-notes="${id}"] [data-attachment] img`)
      return el instanceof HTMLImageElement && el.complete && el.naturalWidth > 0
    }, noted)
    check(md.includes(`](attachments/${noted}/`), 'an image saved with the notes shows inline')
  }

  // Merge: a duplicate leaves the lists, links to it point to the one that stays, and that page
  // lists it as merged from.
  const keepTask = idOf(para(...v, 'task', 'add', 'Renew the cert', '-p', proj, '--json'))
  const dupTask = idOf(para(...v, 'task', 'add', 'Renew TLS cert', '-p', proj, '--json'))
  const linker = idOf(
    para(...v, 'note', 'add', 'Cert plan', '--body', `See [[${dupTask}]].`, '-p', proj, '--json'),
  )
  await agent.call('doc_merge', { keep: keepTask, alias: dupTask, why: 'The same cert.' })
  check(
    !para(...v, 'task', 'list', '--json').includes(dupTask),
    'a merged duplicate leaves the task list',
  )
  check(
    para(...v, 'note', 'show', linker, '--json').includes(`[[${keepTask}]]`),
    'a link to the merged duplicate points to the one that stays',
  )
  await rpage.goto(`${base}/doc/${keepTask}`)
  await rpage.locator(`[data-merged-from] >> text=Renew TLS cert`).waitFor()
  await rpage.goto(`${base}/doc/${dupTask}`)
  await rpage.locator(`[data-merged-into="${keepTask}"]`).waitFor()
  check(true, 'the kept page lists the duplicate as merged from; the duplicate says where it went')

  // Agenda: Done on a task row closes the task and the row leaves the agenda.
  const agendaTask = idOf(
    para(...v, 'task', 'add', 'Close from the agenda', '-p', proj, '--due', '2020-01-01', '--json'),
  )
  await rpage.goto(`${base}/agenda`)
  const agendaRow = rpage.locator(`[data-agenda-row="${agendaTask}"]`)
  await agendaRow.waitFor()
  await agendaRow.getByRole('button', { name: 'Done' }).click()
  await agendaRow.waitFor({ state: 'detached' })
  const doneToast = rpage.locator('[data-toast]').filter({ hasText: 'Marked done' }).first()
  await doneToast.waitFor()
  check(
    /"status":\s*"done"/.test(para(...v, 'task', 'show', agendaTask, '--json')),
    'Done on an agenda task closes it and the row leaves the agenda',
  )
  // Undo in the toast undoes the close from its receipt: the task is open again.
  await doneToast.getByRole('button', { name: 'Undo' }).click()
  await rpage.locator('[data-toast]').filter({ hasText: 'Undone' }).first().waitFor()
  check(
    /"status":\s*"todo"/.test(para(...v, 'task', 'show', agendaTask, '--json')),
    'Undo of Done on the agenda opens the task again',
  )
  // Reopen on a closed task's page opens it again.
  para(...v, 'close', agendaTask)
  await rpage.goto(`${base}/doc/${agendaTask}`)
  await rpage.locator(`[data-item-actions="${agendaTask}"] [data-act="reopen"]`).click()
  await rpage.locator('[data-toast]').filter({ hasText: 'Reopened' }).first().waitFor()
  check(
    /"status":\s*"todo"/.test(para(...v, 'task', 'show', agendaTask, '--json')),
    'Reopen on a closed task page opens it again',
  )

  // Omni bar: / focuses it, typing lists results, a pasted id and Enter open the document.
  await rpage.goto(`${base}/agenda`)
  const omni = rpage.getByRole('combobox', { name: 'Search' })
  await omni.waitFor()
  await rpage.waitForLoadState('networkidle').catch(() => {})
  await rpage.keyboard.press('/')
  await rpage.waitForFunction(() => document.activeElement?.getAttribute('role') === 'combobox')
  await omni.fill(task)
  await rpage.getByRole('option').first().waitFor()
  await rpage.keyboard.press('Enter')
  await rpage.waitForURL((u) => u.pathname === `/doc/${task}`)
  check(true, '/ focuses the omni bar; a pasted id and Enter open the document')
  check(
    rerrors.length === 0,
    `the review action pages log no console errors${rerrors.length ? `: ${rerrors.join(' | ')}` : ''}`,
  )

  // para serve login: a login key one day from its end renews on the first page load, and the
  // browser stays signed in with review:decide.
  const loginUrl = para(...v, 'serve', 'login', '--print', '--days', '1').trim()
  const oldKey = /#token=para_(viewer-[a-z0-9-]+?)_/.exec(decodeURIComponent(loginUrl))?.[1]
  check(!!oldKey && loginUrl.startsWith(`${base}/#token=`), 'serve login prints a sign-in URL')
  const lctx = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const lpage = await lctx.newPage()
  watched = lpage
  const renewed = lpage.waitForResponse((r) => r.url().endsWith('/auth/session/renew'))
  await lpage.goto(loginUrl)
  await lpage.waitForURL(/\/next/)
  check((await renewed).status() === 200, 'the web app renews a login key near its end')
  const me = await lpage.evaluate(async () => (await fetch('/auth/session')).json())
  check(
    me.key !== oldKey && me.login === true && (me.scopes ?? []).includes('review:decide'),
    `after renew the session is a new login key with review:decide (${JSON.stringify(me)})`,
  )
  const sessions = JSON.parse(para(...v, 'serve', 'sessions', '--json'))
  check(
    sessions.length === 1 && sessions[0].id === me.key,
    'serve sessions lists the new key only; the old one is revoked',
  )
  await lpage.reload()
  await lpage.getByRole('heading', { name: 'Next', exact: true }).waitFor()
  check(
    (await lpage.locator('[data-banner="read-only"]').count()) === 0,
    'after renew the page is still signed in and can decide',
  )
  para(...v, 'serve', 'logout')
  await lpage.reload()
  await lpage.getByRole('heading', { name: 'Not signed in' }).waitFor()
  check(true, 'after logout the page says not signed in')
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
