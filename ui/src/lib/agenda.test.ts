import { describe, expect, it } from 'vitest'
import {
  agendaActions,
  closeBody,
  datedKind,
  doneLabel,
  investigable,
  isClosed,
  openTarget,
  rowKind,
  splitDone,
} from './agenda'

describe('agendaActions', () => {
  it('gives each open kind the actions that fit it', () => {
    expect(agendaActions('task')).toEqual({ done: 'task', later: true, reopen: false })
    expect(agendaActions('goal')).toEqual({ done: 'task', later: true, reopen: false })
    expect(agendaActions('meeting')).toEqual({ done: 'meeting', later: false, reopen: false })
    expect(agendaActions('ask')).toEqual({ done: 'ask', later: false, reopen: false })
    expect(agendaActions('question')).toEqual({ done: 'drop', later: true, reopen: false })
    expect(agendaActions('io')).toEqual({ done: 'drop', later: true, reopen: false })
    expect(agendaActions('incident')).toEqual({ done: 'resolve', later: false, reopen: false })
    expect(agendaActions(undefined)).toEqual({ done: null, later: false, reopen: false })
    expect(agendaActions('constraint')).toEqual({ done: null, later: false, reopen: false })
  })
  it('gives a closed row Reopen only', () => {
    expect(agendaActions('task', true)).toEqual({ done: null, later: false, reopen: true })
    expect(agendaActions('question', true)).toEqual({ done: null, later: false, reopen: true })
    expect(agendaActions('constraint', true)).toEqual({ done: null, later: false, reopen: false })
  })
  it('gives a row put off Reopen (bring back) instead of Later', () => {
    expect(agendaActions('task', false, true)).toEqual({ done: 'task', later: false, reopen: true })
  })
  it('opens each kind on its own page', () => {
    expect(openTarget('incident')).toBe('incident')
    expect(openTarget('ask')).toBe('rfx')
    expect(openTarget('io')).toBe('io')
    expect(openTarget('task')).toBe('doc')
  })
})

describe('rowKind and isClosed', () => {
  it('names a document by the kind its row acts on', () => {
    expect(rowKind('task')).toBe('task')
    expect(rowKind('task', 'meeting')).toBe('meeting')
    expect(rowKind('project')).toBe('goal')
    expect(rowKind('rfx')).toBe('ask')
    expect(rowKind('question')).toBe('question')
  })
  it('knows the closed status values', () => {
    for (const s of ['done', 'dropped', 'answered', 'closed', 'declined', 'resolved', 'sent'])
      expect(isClosed(s)).toBe(true)
    for (const s of ['todo', 'doing', 'blocked', 'open', 'active', 'draft', 'unread', undefined])
      expect(isClosed(s)).toBe(false)
  })
})

describe('closeBody', () => {
  it('drops with the reason, holds a meeting with notes, closes a task plainly', () => {
    expect(closeBody('drop', 'question-a', ' Moot. ')).toEqual({
      id: 'question-a',
      outcome: 'dropped',
      reason: 'Moot.',
    })
    expect(closeBody('meeting', 'task-m', 'Agreed.')).toEqual({ id: 'task-m', reason: 'Agreed.' })
    expect(closeBody('meeting', 'task-m', '')).toEqual({ id: 'task-m', reason: undefined })
    expect(closeBody('task', 'task-a')).toEqual({ id: 'task-a' })
    expect(closeBody('ask', 'rfx-a', 'ignored')).toEqual({ id: 'rfx-a' })
  })
  it('labels the button by what it does', () => {
    expect(doneLabel('task')).toBe('Done')
    expect(doneLabel('drop')).toBe('Drop')
    expect(doneLabel('resolve')).toBe('Resolved')
  })
})

describe('datedKind', () => {
  it('maps dated agenda kinds to row kinds', () => {
    expect(datedKind('due')).toBe('task')
    expect(datedKind('nudge')).toBe('ask')
    expect(datedKind('meeting')).toBe('meeting')
    expect(datedKind('exception')).toBe('')
  })
})

describe('splitDone', () => {
  it('keeps order and puts closed rows apart', () => {
    const rows = [
      { id: 'a', s: 'todo' },
      { id: 'b', s: 'done' },
      { id: 'c', s: 'doing' },
    ]
    const { open, done } = splitDone(rows, (r) => isClosed(r.s))
    expect(open.map((r) => r.id)).toEqual(['a', 'c'])
    expect(done.map((r) => r.id)).toEqual(['b'])
  })
})

describe('investigable', () => {
  it('gives Investigate to tasks, obligations, asks, and goals', () => {
    for (const k of ['task', 'obligation', 'ask', 'goal']) expect(investigable(k)).toBe(true)
    for (const k of ['question', 'meeting', 'io', 'incident', '', undefined])
      expect(investigable(k)).toBe(false)
  })
})
