// What the owner can do to a row of the agenda, by its kind. Done closes it (item_close: a task is
// done, a meeting held, an ask the owner made closed); Later puts it off (stack_later); Open goes
// to its page, where the composer is. A question is answered on its page or the stack, an incident
// is worked in its page, and a meeting cannot be put off: it has a time.
export type AgendaActionSet = { done: 'task' | 'meeting' | 'ask' | null; later: boolean }

export function agendaActions(kind: string | null | undefined): AgendaActionSet {
  switch (kind) {
    case 'task':
      return { done: 'task', later: true }
    case 'meeting':
      return { done: 'meeting', later: false }
    case 'ask':
      return { done: 'ask', later: false }
    case 'question':
    case 'io':
      return { done: null, later: true }
    default:
      return { done: null, later: false }
  }
}

// openTarget is the page an agenda row opens.
export function openTarget(kind: string | null | undefined): 'incident' | 'rfx' | 'io' | 'doc' {
  if (kind === 'incident') return 'incident'
  if (kind === 'ask') return 'rfx'
  if (kind === 'io') return 'io'
  return 'doc'
}
