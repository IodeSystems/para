import type { Answer } from '@/lib/dossier'

// Helpers for the review actions every card shares: Approve, Edit, Decline, Skip, Ask agent, and
// Done by me. The rules here copy model.CheckAnswer and the person ops.

// Decision is what the owner decides on an item. Decline is decision reject.
export type Decision = 'approve' | 'edit' | 'reject' | 'skip' | 'ask'

// needsNote says whether a decision needs the owner's note: a decline says why, an ask says what
// the agent should do.
export const needsNote = (decision: string | null | undefined) =>
  decision === 'reject' || decision === 'decline' || decision === 'ask'

// noteProblem says what is missing from a decision and its note, or '' when nothing is.
export function noteProblem(decision: string | null | undefined, note: string | null | undefined) {
  if (!needsNote(decision) || (note ?? '').trim() !== '') return ''
  return decision === 'ask'
    ? 'Ask agent needs a note: what the agent should do.'
    : 'Decline needs a note: why.'
}

// answerProblem says why an answer cannot be saved or submitted, or '' when it can.
export const answerProblem = (a: Answer | null | undefined) =>
  a ? noteProblem(a.decision, a.comment) : ''

// noteLabel is the label of the note field for a decision.
export function noteLabel(decision: string | null | undefined): string {
  switch (decision) {
    case 'reject':
    case 'decline':
      return 'Why you decline (required)'
    case 'ask':
      return 'What the agent should do (required)'
    case 'skip':
      return 'Note (optional)'
    default:
      return 'Comment for the agent'
  }
}

// EDIT_KEYS are the answer keys that hold the owner's edited text.
const EDIT_KEYS = ['input', 'subject', 'body', 'people', 'new'] as const
// QUESTION_KEYS are the answer keys of a question's answer.
const QUESTION_KEYS = ['choices', 'addedOptions', 'editedOptions', 'ownWords'] as const

// withDecision sets a decision on an answer and drops what that decision does not take: skip and
// ask take only the note (ask also refs), approve and decline take no edited text, and edit
// starts from the owner's text or else from start. The same decision again takes it back.
export function withDecision(a: Answer | null, decision: Decision, start?: Answer): Answer {
  const cur = a ?? {}
  if (cur.decision === decision) {
    const out: Answer = { ...cur, decision: null }
    for (const k of EDIT_KEYS) delete out[k]
    delete out.refs
    return out
  }
  const out: Answer = { ...cur, decision }
  if (decision !== 'ask') delete out.refs
  if (decision === 'skip' || decision === 'ask') {
    for (const k of [...EDIT_KEYS, ...QUESTION_KEYS]) delete out[k]
    delete out.comments
    return out
  }
  if (decision === 'edit') {
    const has = EDIT_KEYS.some((k) => cur[k] !== undefined && cur[k] !== null)
    return has && cur.decision === 'edit' ? out : { ...out, ...start }
  }
  for (const k of EDIT_KEYS) delete out[k]
  return out
}

// draftedEdits is the owner's edited text and question picks in an answer, or null when there are
// none: what a skip or an ask sets aside.
export function draftedEdits(a: Answer | null | undefined): Answer | null {
  if (!a) return null
  const out: Answer = {}
  for (const k of [...EDIT_KEYS, ...QUESTION_KEYS]) {
    if (a[k] !== undefined && a[k] !== null) Object.assign(out, { [k]: a[k] })
  }
  return Object.keys(out).length > 0 ? out : null
}

// parseRefs reads vault ids from text: separated by commas, spaces, or new lines. [[id]] is id.
export function parseRefs(text: string): string[] {
  const out: string[] = []
  for (const raw of text.split(/[\s,]+/)) {
    const id = raw.replace(/^\[\[|\]\]$/g, '').trim()
    if (id && !out.includes(id)) out.push(id)
  }
  return out
}

// DONE_KINDS are the outward kinds the owner may mark done by themself.
const DONE_KINDS = new Set(['message', 'external', 'action', 'doc_edit'])
// DONE_STATES are the states an item may be marked done from (proposal_done).
const DONE_STATES = new Set(['pending', 'held', 'skipped', 'sent_back', 'approved', 'claimed'])

// canMarkDone says whether the owner may mark an item done by themself.
export const canMarkDone = (it: { kind: string; state: string }) =>
  DONE_KINDS.has(it.kind) && DONE_STATES.has(it.state)

// FieldKind is how the input form edits one top-level key of an op's input.
export type FieldKind = 'text' | 'number' | 'bool' | 'json'

// fieldKind says how to edit a value: a string as text, a number, a boolean as a check box, and
// anything else (a list, an object, null) as JSON text.
export function fieldKind(v: unknown): FieldKind {
  if (typeof v === 'string') return 'text'
  if (typeof v === 'number') return 'number'
  if (typeof v === 'boolean') return 'bool'
  return 'json'
}

// fieldText is a value as the form shows it.
export function fieldText(v: unknown): string {
  switch (fieldKind(v)) {
    case 'text':
      return v as string
    case 'number':
    case 'bool':
      return String(v)
    default:
      return v === undefined ? '' : JSON.stringify(v, null, 2)
  }
}

export type Parsed = { ok: true; value: unknown } | { ok: false; error: string }

// parseField reads the text of one field back to a value of its kind.
export function parseField(kind: FieldKind, text: string): Parsed {
  switch (kind) {
    case 'text':
      return { ok: true, value: text }
    case 'bool':
      return { ok: true, value: text === 'true' }
    case 'number': {
      const n = Number(text)
      if (text.trim() === '' || !Number.isFinite(n)) return { ok: false, error: 'Give a number.' }
      return { ok: true, value: n }
    }
    default:
      if (text.trim() === '') return { ok: true, value: undefined }
      try {
        return { ok: true, value: JSON.parse(text) as unknown }
      } catch {
        return { ok: false, error: 'This is not JSON yet. Para keeps the last good value.' }
      }
  }
}

// inputObject reads an op input as an object; anything else is an empty object.
export function inputObject(v: unknown): Record<string, unknown> {
  return v && typeof v === 'object' && !Array.isArray(v)
    ? { ...(v as Record<string, unknown>) }
    : {}
}

// setInputKey gives the input with one key set; undefined removes the key.
export function setInputKey(input: unknown, key: string, value: unknown): Record<string, unknown> {
  const out = inputObject(input)
  if (value === undefined) delete out[key]
  else out[key] = value
  return out
}
