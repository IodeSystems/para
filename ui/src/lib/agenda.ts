// What the owner can do to an item row, by its kind: Done closes it (close: a task is done, a
// meeting held, an ask closed, a goal done, an incident resolved; a question or an io item is
// dropped with a reason), Later puts it off (later), Reopen opens a closed item again (reopen),
// and Open goes to its page, where the composer is. A meeting and an ask cannot be put off: a
// meeting has a time, an ask its nudge date.
export type DoneKind = 'task' | 'meeting' | 'ask' | 'drop' | 'resolve' | null
export type AgendaActionSet = { done: DoneKind; later: boolean; reopen: boolean }

const OPEN: Record<string, AgendaActionSet> = {
  task: { done: 'task', later: true, reopen: false },
  goal: { done: 'task', later: true, reopen: false },
  meeting: { done: 'meeting', later: false, reopen: false },
  ask: { done: 'ask', later: false, reopen: false },
  question: { done: 'drop', later: true, reopen: false },
  io: { done: 'drop', later: true, reopen: false },
  incident: { done: 'resolve', later: false, reopen: false },
}

const NONE: AgendaActionSet = { done: null, later: false, reopen: false }

// agendaActions gives an open row Done and Later as its kind allows, and a closed row Reopen. A row
// put off (deferred) keeps Done and gets Reopen (bring it back now) instead of Later.
export function agendaActions(
  kind: string | null | undefined,
  closed = false,
  deferred = false,
): AgendaActionSet {
  const open = OPEN[kind ?? '']
  if (!open) return NONE
  if (closed) return { done: null, later: false, reopen: true }
  return deferred ? { ...open, later: false, reopen: true } : open
}

// rowKind is the row kind of a vault document: its type, a task of kind meeting as a meeting, a
// project as a goal, an rfx as an ask.
export function rowKind(type: string | null | undefined, taskKind?: string | null): string {
  switch (type) {
    case 'task':
      return taskKind === 'meeting' ? 'meeting' : 'task'
    case 'project':
      return 'goal'
    case 'rfx':
      return 'ask'
    default:
      return type ?? ''
  }
}

// CLOSED are the status values of a closed item, by type.
const CLOSED = new Set([
  'done',
  'dropped',
  'answered',
  'closed',
  'declined',
  'resolved',
  'held',
  'cancelled',
  'sent',
  'filed',
])

// isClosed says whether a status value is a closed one.
export function isClosed(status: string | null | undefined): boolean {
  return CLOSED.has(status ?? '')
}

// doneLabel is the word on the Done button of a kind.
export function doneLabel(done: DoneKind): string {
  switch (done) {
    case 'drop':
      return 'Drop'
    case 'resolve':
      return 'Resolved'
    default:
      return 'Done'
  }
}

// closeBody is the close call of a Done: text is the owner's words (meeting notes, why it is
// dropped), optional except for a drop.
export function closeBody(
  done: DoneKind,
  id: string,
  text = '',
): { id: string; outcome?: string; reason?: string } {
  const reason = text.trim() || undefined
  switch (done) {
    case 'drop':
      return { id, outcome: 'dropped', reason }
    case 'meeting':
    case 'resolve':
      return { id, reason }
    case 'task':
      return reason ? { id, reason } : { id }
    default:
      return { id }
  }
}

// datedKind is the row kind of a dated agenda item (agenda by date): a meeting, a task due, an ask
// to follow up, a goal to review. An exception or an area to review has no Done here.
export function datedKind(kind: string): string {
  switch (kind) {
    case 'due':
      return 'task'
    case 'nudge':
      return 'ask'
    case 'meeting':
    case 'goal':
      return kind
    default:
      return ''
  }
}

// openTarget is the page an agenda row opens.
export function openTarget(kind: string | null | undefined): 'incident' | 'rfx' | 'io' | 'doc' {
  if (kind === 'incident') return 'incident'
  if (kind === 'ask') return 'rfx'
  if (kind === 'io') return 'io'
  return 'doc'
}

// splitDone puts open rows first and returns the closed ones apart, for a list that hides done
// items until Show done.
export function splitDone<T>(rows: T[], closed: (r: T) => boolean): { open: T[]; done: T[] } {
  const open: T[] = []
  const done: T[] = []
  for (const r of rows) (closed(r) ? done : open).push(r)
  return { open, done }
}
