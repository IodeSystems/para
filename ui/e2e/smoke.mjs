// Smoke test against the real binary. Agents write through MCP; the owner only reviews in the web
// app. It builds para with the embedded UI (or uses PARA_E2E_BIN), makes a temp install with one
// synthetic vault, seeds it with the CLI and the MCP server, runs `para serve` on 127.0.0.1, signs
// in Chrome with a read-only key, and checks that every main page loads, shows no write control,
// and logs no console error. Then it signs in with a review:decide key and answers a dossier:
// draft, reload, submit, and the stack state chips. Needs Google Chrome (playwright channel 'chrome') and a built ui/dist
// (`pnpm build`) when it builds the binary.
//
//   pnpm build && PARA_E2E_PORT=8245 pnpm e2e
//
// It uses one port (7710, or PARA_E2E_PORT), a temp home, and stops the server and the MCP agent
// by their own PIDs.
import { spawn, execFileSync } from 'node:child_process'
import { existsSync, mkdtempSync, openSync, readFileSync, rmSync } from 'node:fs'
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
  // The seed's agent writes apply at once: the gate that holds them for review is tested in Go.
  para(...v, 'vault', 'config', 'set', 'gate.require', 'none')
  const proj = idOf(
    para(...v, 'project', 'add', 'Launch', '--owner', 'me', '--done-when', 'Launched.', '--json'),
  )
  const task = idOf(para(...v, 'task', 'add', 'Write the plan', '-p', proj, '--json'))
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
  const meeting = idOf(
    para(...v, 'meeting', 'add', 'Quota sync', '--with', person, '--rfx', ask, '--json'),
  )
  para(...v, 'inbox', 'add', 'Seed: ask platform about the quota')

  // Agent writes go through MCP, as they do for real: each leaves a receipt.
  agent = startAgent()
  const note = await agent.call('note_add', {
    title: 'Quota numbers',
    body: 'Staging uses 80 percent of its quota.',
    project: proj,
    why: 'Sam asked for the numbers in one place.',
    sources: ['https://example.com/quota-dashboard'],
    verify: 'Open the note.',
  })
  const acted = await agent.call('action_record', {
    what: 'Opened ticket OPS-1 for the quota',
    where: 'OPS ticket queue',
    url: 'https://tickets.example.com/OPS-1',
    owner_said: 'yes, open the ticket',
  })
  const proposal = await agent.call('proposal_add', {
    title: 'Quota follow-ups',
    tldr: 'Email Sam and add a follow-up task.',
    context: 'The quota runs out next week.',
    source: note.id,
    items: [
      {
        kind: 'message',
        people: [person],
        channel: 'email',
        subject: 'Staging quota',
        body: 'Hi Sam, could you raise the staging quota?',
        reversible: 'no',
        pros: [{ text: 'Sam owns the quota', source: 'https://example.com/owners' }],
        confidence: 80,
        confidence_basis: 'Sam answered quota asks before',
        evidence: { quote: 'Staging uses 80 percent', locator: 'note body' },
      },
      {
        kind: 'para',
        op: 'task_add',
        input: { title: 'Follow up on quota', project: proj, done_when: 'The ticket is open.' },
        how: 'Adds a task under Launch.',
        impact: 'The follow-up shows on the Agenda.',
        reversible: 'yes',
        evidence: { quote: 'Staging uses 80 percent', locator: 'note body' },
      },
    ],
  })
  check(!!note.id && !!acted.receipt && !!proposal.id, 'MCP seeds a note, an action, a proposal')

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
    query: 'mutation { para { v1 { inboxAdd(body: { text: "should be refused" }) { id } } } }',
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
        !body.data?.para?.v1?.inboxAdd?.id)
    check(
      refused,
      `a GraphQL mutation with a vault:read key is refused (${gqlWrite.status}: ${gqlWrite.text.slice(0, 160)})`,
    )
  }
  check(
    !para(...v, 'inbox', 'list').includes('should be refused'),
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

  const pages = [
    ['/agenda', 'Agenda'],
    ['/agenda?by=date', 'Agenda'],
    ['/writes', 'Recent agent writes'],
    ['/work', 'Work'],
    ['/rfx', 'Asks'],
    [`/rfx/${ask}`, 'Need the staging quota raised'],
    ['/people', 'People'],
    [`/people/${person}`, 'Sam Rivera'],
    ['/policy', 'Policy'],
    ['/questions', 'Questions'],
    ['/requests', 'Agent work'],
    ['/meetings', 'Meetings'],
    [`/meetings/${meeting}`, 'Quota sync'],
    ['/activity', 'Agent activity'],
    ['/friction', 'Friction'],
    ['/health', 'Health'],
    ['/proposals', 'Proposals'],
    ['/stack', 'Stack'],
    [`/proposals/${proposal.id}`, 'Quota follow-ups'],
    [`/projects/${proj}`, 'Launch'],
    [`/doc/${task}`, 'Write the plan'],
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
      .getByText('Decide what agents propose', { exact: false })
      .count()) === 1,
    'help says where to decide',
  )
  await page.keyboard.press('Escape')

  // Recent agent writes: receipts newest first, the self-reported mark, owner said, undo command.
  await page.goto(`${base}/writes`)
  await page.getByRole('heading', { name: 'Recent agent writes' }).waitFor()
  const card = page.locator('[data-receipt]', { hasText: 'Quota numbers' }).first()
  await card.getByText('Sam asked for the numbers in one place.').waitFor()
  check(true, 'a write shows its why')
  check(
    (await card.getByText('https://example.com/quota-dashboard').count()) > 0,
    'a write shows its sources',
  )
  const actCard = page.locator(`[data-receipt="${acted.receipt}"]`)
  await actCard.waitFor()
  check((await actCard.getByText('self-reported').count()) === 1, 'action_record is self-reported')
  check(
    (await actCard.getByText(/Owner said: .yes, open the ticket./).count()) === 1,
    'the owner-said words show',
  )
  check(
    (await actCard.getByText(`para receipt undo ${acted.receipt}`).count()) === 1,
    'the undo command shows',
  )
  check(
    (await actCard.getByRole('button', { name: 'Copy undo command' }).count()) === 1,
    'the undo command has a copy button',
  )
  {
    const ids = await page
      .locator('[data-receipt]')
      .evaluateAll((els) => els.map((e) => e.getAttribute('data-receipt')))
    const noteRid = await card.getAttribute('data-receipt')
    check(ids.indexOf(acted.receipt) < ids.indexOf(noteRid), 'the newest write is first')
  }
  await card.locator('summary', { hasText: 'What changed' }).click()
  await card.getByLabel('What this write changed').waitFor()
  check(true, 'a write opens its before and after')
  check(
    (await page.getByRole('link', { name: 'Quota follow-ups' }).count()) > 0,
    'Waiting for your review links the open proposal',
  )
  check((await page.getByRole('link', { name: 'All proposals' }).count()) === 1, 'links /proposals')

  // The proposal page: tldr, items as the real thing, untrusted evidence, no decide control.
  await page.goto(`${base}/proposals/${proposal.id}`)
  await page.getByRole('heading', { name: 'Quota follow-ups' }).waitFor()
  check(
    (await page.getByText('Email Sam and add a follow-up task.').count()) === 1,
    'shows the tldr',
  )
  check((await page.locator('[data-item]').count()) === 2, 'shows both items')
  check(
    (await page.getByText('Hi Sam, could you raise the staging quota?').count()) === 1,
    'the message item shows its body',
  )
  check((await page.getByText('Staging quota').count()) > 0, 'the message item shows its subject')
  check(
    (await page.getByText('Sam owns the quota', { exact: false }).count()) > 0,
    'shows the pros',
  )
  check((await page.getByText('untrusted').count()) >= 2, 'evidence is marked untrusted')
  check(
    (await page
      .getByRole('button', { name: /^(approve|reject|deny|request|hold|edit)/i })
      .count()) === 0,
    'with a read-only key the proposal page has no decide buttons',
  )

  // Sign out works and lands on the sign-in page.
  await page.getByRole('button', { name: 'Sign out' }).click()
  await page.waitForURL(/\/login/)
  check(true, 'sign out returns to /login')

  check(
    writes.length === 0,
    `the read-only browser sent no write${writes.length ? `: ${writes.join(' | ')}` : ''}`,
  )

  // The review flow: a dossier filed by an agent, answered by the owner with a review:decide key.
  const dossier = await agent.call('proposal_add', {
    title: 'Vendor pick',
    markdown: [
      '```yaml para-dossier',
      'tldr: Pick a vendor',
      '```',
      '',
      '## Findings',
      '',
      'Two vendors answered.',
      '',
      '```yaml para-question id=q1',
      'title: Which vendor?',
      'options: [A, B]',
      'recommended: B',
      'evidence: {quote: "B costs less", locator: "https://acme.example/q"}',
      '```',
      '',
      '```yaml para-message id=m1',
      'people: [ada@acme.example]',
      'body: Hi Ada.',
      'evidence: {quote: q, locator: l}',
      '```',
      '',
    ].join('\n'),
  })
  check(!!dossier.id, 'MCP files a dossier')
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
  await rpage.waitForURL(/\/agenda/)
  await rpage.goto(`${base}/proposals/${dossier.id}`)
  await rpage.getByRole('heading', { name: 'Vendor pick' }).waitFor()
  const q1 = rpage.locator('[data-item="q1"]')
  const m1 = rpage.locator('[data-item="m1"]')
  await q1.waitFor()
  check((await m1.count()) === 1, 'the dossier shows each block as a card')
  check(
    (await q1.getByRole('link', { name: 'https://acme.example/q' }).count()) === 1,
    'an evidence address is a link',
  )
  check((await rpage.locator('[data-drafted="0/2"]').count()) === 1, 'the footer says 0 of 2')

  // Draft: accept the recommended option, approve the message, comment on the section.
  const saved = rpage.waitForResponse(
    (r) =>
      r.url().includes('/api/graphql') && (r.request().postData() ?? '').includes('ProposalDraft'),
  )
  await q1.getByRole('button', { name: /Accept the recommended/ }).click()
  await m1.getByRole('button', { name: 'Approve', exact: true }).click()
  await rpage.getByLabel('Comment on Findings').fill('Check the price again.')
  await rpage.locator('[data-drafted="2/2"]').waitFor()
  await saved
  await rpage.locator('[data-save="saved"]').waitFor()
  check(true, 'the answers save as a draft')

  // Reload: the draft is read back from the file.
  await rpage.reload()
  await q1.waitFor()
  check(await q1.getByRole('radio', { name: 'B' }).isChecked(), 'after reload B is still chosen')
  check(
    (await m1
      .getByRole('button', { name: 'Approve', exact: true })
      .getAttribute('aria-pressed')) === 'true',
    'after reload the message is still approved',
  )
  check(
    (await rpage.getByLabel('Comment on Findings').inputValue()) === 'Check the price again.',
    'after reload the section comment is still there',
  )
  check((await rpage.locator('[data-drafted="2/2"]').count()) === 1, 'the footer says 2 of 2')

  // Submit: the answers become decisions.
  await rpage.getByRole('button', { name: 'Submit', exact: true }).click()
  await rpage.getByText('Submitted.', { exact: false }).waitFor()
  await rpage.locator('[data-item="m1"][data-stack="decided"]').waitFor()
  check(
    (await rpage.locator('[data-item="q1"][data-stack="decided"]').count()) === 1,
    'after submit the question is decided',
  )
  check(true, 'after submit the message is decided, waiting for an agent')

  // The stack: state chips count the items, and a click on one shows only those.
  await rpage.goto(`${base}/stack`)
  await rpage.getByRole('heading', { name: 'Stack' }).waitFor()
  await rpage.locator('[data-row]').first().waitFor()
  const chip = (s) => rpage.locator(`[data-state-chip="${s}"]`)
  check((await chip('decided').innerText()).startsWith('2 '), 'the decided chip counts 2')
  check((await chip('proposed').innerText()).startsWith('2 '), 'the proposed chip counts 2')
  await chip('decided').click()
  await rpage.waitForURL(/state=decided/)
  await rpage.locator(`[data-row="${dossier.id}/m1"]`).waitFor()
  check(
    (await rpage.locator('[data-row][data-stack="proposed"]').count()) === 0,
    'the decided chip shows only decided items',
  )
  await chip('decided').click()
  await rpage.waitForURL((u) => !u.search.includes('state='))
  // One click approves an item across dossiers: the vault change runs at once.
  const taskRow = rpage.locator(`[data-row^="${proposal.id}/"][data-kind="para"]`)
  if ((await taskRow.count()) > 0) {
    await taskRow.first().getByRole('button', { name: 'Approve', exact: true }).click()
    await rpage.waitForFunction(() =>
      document.querySelector('[data-state-chip="done"]')?.textContent?.startsWith('1 '),
    )
    check(true, 'approve on the stack runs the vault change')
  } else {
    check(false, 'the stack lists the task_add item of the first proposal')
  }
  check(
    rerrors.length === 0,
    `the review pages log no console errors${rerrors.length ? `: ${rerrors.join(' | ')}` : ''}`,
  )
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
