import { describe, expect, it } from 'vitest'
import { answerInput } from '@/lib/dossier'
import {
  answerProblem,
  draftedEdits,
  canMarkDone,
  fieldKind,
  fieldText,
  noteProblem,
  parseField,
  parseRefs,
  setInputKey,
  withDecision,
} from '@/lib/review'

describe('review notes', () => {
  it('a decline and an ask need a note; skip and approve do not', () => {
    expect(noteProblem('reject', '')).toMatch(/Decline needs a note/)
    expect(noteProblem('decline', '  ')).toMatch(/Decline needs a note/)
    expect(noteProblem('ask', undefined)).toMatch(/Ask agent needs a note/)
    expect(noteProblem('reject', 'Too early.')).toBe('')
    expect(noteProblem('skip', '')).toBe('')
    expect(noteProblem('approve', null)).toBe('')
    expect(answerProblem({ decision: 'ask', comment: 'Check the ticket.' })).toBe('')
    expect(answerProblem({ decision: 'reject' })).not.toBe('')
    expect(answerProblem(null)).toBe('')
  })
  it('reads refs from text', () => {
    expect(parseRefs('task-a1b2c3d4, [[note-x]]\nnote-x  q-1')).toEqual([
      'task-a1b2c3d4',
      'note-x',
      'q-1',
    ])
    expect(parseRefs(' , ')).toEqual([])
  })
  it('marks done only outward items not closed', () => {
    expect(canMarkDone({ kind: 'message', state: 'pending' })).toBe(true)
    expect(canMarkDone({ kind: 'doc_edit', state: 'approved' })).toBe(true)
    expect(canMarkDone({ kind: 'para', state: 'pending' })).toBe(false)
    expect(canMarkDone({ kind: 'question', state: 'pending' })).toBe(false)
    expect(canMarkDone({ kind: 'action', state: 'done' })).toBe(false)
  })
})

describe('withDecision', () => {
  it('skip and ask keep only the note (and refs for ask)', () => {
    const a = { choices: ['B'], ownWords: 'x', comment: 'later', refs: ['t-1'] }
    expect(withDecision(a, 'skip')).toEqual({ decision: 'skip', comment: 'later' })
    expect(withDecision(a, 'ask')).toEqual({ decision: 'ask', comment: 'later', refs: ['t-1'] })
  })
  it('edit starts from the agent version; approve and decline drop edited text', () => {
    const start = { subject: 'S', body: 'B', people: ['p'] }
    const e = withDecision({ comment: 'c' }, 'edit', start)
    expect(e).toEqual({ decision: 'edit', comment: 'c', subject: 'S', body: 'B', people: ['p'] })
    expect(withDecision({ ...e, body: 'mine' }, 'approve')).toEqual({
      decision: 'approve',
      comment: 'c',
    })
    expect(withDecision(e, 'reject')).toEqual({ decision: 'reject', comment: 'c' })
  })
  it('the same decision again takes it back', () => {
    expect(withDecision({ decision: 'skip', comment: 'n' }, 'skip')).toEqual({
      decision: null,
      comment: 'n',
    })
    expect(answerInput(withDecision({ decision: 'approve' }, 'approve'))).toBeNull()
  })
  it('the draft carries people and refs', () => {
    expect(answerInput({ decision: 'ask', comment: 'n', refs: ['t-1'] })).toEqual({
      decision: 'ask',
      comment: 'n',
      refs: ['t-1'],
    })
    expect(answerInput({ decision: 'edit', people: ['person-a'] })).toEqual({
      decision: 'edit',
      people: ['person-a'],
    })
  })
})

describe('input form', () => {
  it('edits each key by its kind', () => {
    expect(fieldKind('x')).toBe('text')
    expect(fieldKind(3)).toBe('number')
    expect(fieldKind(false)).toBe('bool')
    expect(fieldKind(['a'])).toBe('json')
    expect(fieldKind({ a: 1 })).toBe('json')
    expect(fieldKind(null)).toBe('json')
  })
  it('round-trips each kind', () => {
    const input = { title: 'T', n: 2, on: true, tags: ['a', 'b'], meta: { k: 1 } }
    let out: unknown = input
    for (const [k, v] of Object.entries(input)) {
      const r = parseField(fieldKind(v), fieldText(v))
      expect(r.ok).toBe(true)
      if (r.ok) out = setInputKey(out, k, r.value)
    }
    expect(out).toEqual(input)
  })
  it('refuses a bad number or bad JSON', () => {
    expect(parseField('number', 'abc').ok).toBe(false)
    expect(parseField('number', '').ok).toBe(false)
    expect(parseField('json', '[1,').ok).toBe(false)
    expect(parseField('json', '')).toEqual({ ok: true, value: undefined })
  })
  it('an undefined value removes the key, and the input is not changed in place', () => {
    const input = { a: 1, b: 2 }
    expect(setInputKey(input, 'a', undefined)).toEqual({ b: 2 })
    expect(input).toEqual({ a: 1, b: 2 })
    expect(setInputKey(null, 'x', 'y')).toEqual({ x: 'y' })
  })
})

describe('draftedEdits', () => {
  it('keeps edited text and question picks, and nothing else', () => {
    expect(draftedEdits({ decision: 'edit', body: 'B', comment: 'c', choices: ['A'] })).toEqual({
      body: 'B',
      choices: ['A'],
    })
    expect(draftedEdits({ decision: 'approve', comment: 'c' })).toBeNull()
    expect(draftedEdits(null)).toBeNull()
  })
})
