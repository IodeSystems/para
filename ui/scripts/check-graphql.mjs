// Validate every graphql(`…`) snippet in src/ against the committed SDL snapshot (gen/schema.graphql).
// tsc does NOT check these snippets, so this is the only enforcement of query drift. Replaces
// graphql-eslint. Runs graphql-js's full `specifiedRules` (fields-on-correct-type, known-argument-names,
// known-type-names, known-directives, known-fragment-names, fragments-on-composite-type,
// executable-definitions, no-unused-fragments, possible-fragment-spread, provided-required-arguments,
// scalar-leafs, unique-argument-names, values-of-correct-type, variables-are-input-types,
// variables-in-allowed-position, …) — a superset of the graphql-eslint rule list we used to enable.
// (graphql-codegen's own validation skips NoUnusedFragments / KnownDirectives / NoUnusedVariables, so
// `pnpm gen` alone is not enough.)
//
// Usage: node scripts/check-graphql.mjs [file ...]   — with files, only report errors in those files
// (the whole project is still parsed, so cross-file fragments resolve). Exit 1 on any error.
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative, resolve } from 'node:path'
import { buildSchema, parse, Source, specifiedRules, validate } from 'graphql'
import { gqlPluckFromCodeString } from '@graphql-tools/graphql-tag-pluck'

const root = resolve(import.meta.dirname, '..')
const schema = buildSchema(readFileSync(join(root, 'gen/schema.graphql'), 'utf8'))

function* walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) {
      if (p === join(root, 'src/gql')) continue // generated
      yield* walk(p)
    } else if (/\.tsx?$/.test(name)) yield p
  }
}

const only = new Set(process.argv.slice(2).map((f) => resolve(f)))
const definitions = []
let failed = 0
const fail = (where, msg) => {
  failed++
  console.error(`${where}: error graphql: ${msg}`)
}

for (const file of walk(join(root, 'src'))) {
  const code = readFileSync(file, 'utf8')
  if (!/graphql\s*\(/.test(code)) continue
  let snippets
  try {
    snippets = await gqlPluckFromCodeString(file, code)
  } catch (e) {
    fail(relative(root, file), `could not extract graphql snippets: ${e.message}`)
    continue
  }
  for (const s of snippets) {
    const name = `${relative(root, file)}`
    // pluck trims the body and reports the line of the opening backtick, so find where the
    // body's first line really sits to keep reported line numbers exact.
    const first = s.body.split('\n')[0]
    const idx = code
      .split('\n')
      .findIndex((l, i) => i >= s.locationOffset.line - 1 && l.includes(first))
    const startLine = idx >= 0 ? idx + 1 : s.locationOffset.line
    try {
      const doc = parse(new Source(s.body, name, { line: startLine, column: 1 }))
      definitions.push(...doc.definitions)
    } catch (e) {
      fail(`${name}:${e.locations?.[0]?.line ?? '?'}`, e.message)
    }
  }
}

for (const err of validate(schema, { kind: 'Document', definitions }, specifiedRules)) {
  const loc = err.nodes?.[0]?.loc
  const file = loc?.source.name ?? '?'
  if (only.size && !only.has(resolve(root, file))) continue
  const pos = loc ? loc.source.locationOffset.line + (err.locations?.[0]?.line ?? 1) - 1 : '?'
  fail(`${file}:${err.locations?.[0] ? pos : '?'}`, err.message)
}

// The web app does not author: the agent writes through MCP. The only GraphQL mutations it may send
// are vault:read ops that change no document (they POST because they mint a cap or log an egress
// event), and the review:decide person ops: the owner drafts answers, submits them, approves or
// rejects what an agent proposed, answers or sends back an open question, marks an outward item or an
// outbox draft done by the owner, checks or reverts an agent write from its receipt, marks a
// tracked document's change seen, and edits the notes of an item (owner 2026-11-04). The composer on
// every item sends the owner's words to an agent, takes them back while unclaimed, and closes a task
// or a meeting the owner saved notes on (owner 2026-11-05). Later on the stack puts items off until a
// date and its Undo brings them back (owner approved the snooze and topic web actions 2026-11-05).
const READ_MUTATIONS = new Set(['LiveSubscribe', 'CopyDoc', 'CalendarExport'])
const REVIEW_MUTATIONS = new Set([
  'ProposalDraft',
  'ProposalSubmit',
  'StackDecide',
  'StackUndo',
  'DocSeen',
  'ReceiptCheck',
  'ReceiptRevert',
  'QuestionReview',
  'ProposalDone',
  'IODone',
  'NotesSave',
  'AgentSend',
  'AgentUnsend',
  'ItemClose',
  // Attend and Skip on the agenda: the owner says whether they go to a meeting (meeting_rsvp, a
  // person op). Owner approved 2026-11-05.
  'MeetingRsvp',
  'StackLater',
  'StackReturn',
  // owner 2026-11-06: stage 2 generic verbs. Done, Later, and Reopen on every item row, and the
  // composer's send, call the generic ops (close, later, reopen, send_to_agent); each runs the op
  // of the item's type with that op's rules, and leaves a receipt that ReceiptRevert undoes.
  'Close',
  'Later',
  'Reopen',
  'SendToAgent',
])
for (const d of definitions) {
  if (d.kind === 'OperationDefinition' && d.operation === 'mutation') {
    const name = d.name?.value ?? '(anonymous)'
    if (!READ_MUTATIONS.has(name) && !REVIEW_MUTATIONS.has(name)) {
      fail(
        `${d.loc?.source.name ?? '?'}`,
        `mutation ${name}: the web app only reviews; agents write through MCP`,
      )
    }
  }
}

if (failed) {
  console.error(
    `\n${failed} GraphQL error(s) vs gen/schema.graphql. If the backend schema changed, run make schema and pnpm gen.`,
  )
  process.exit(1)
}
