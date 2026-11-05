// Words and rules for what the review stack tells the owner after an action: the toast text, whether
// the action can be undone, and which card takes focus when one leaves.

export type Verb =
  'approve' | 'decline' | 'skip' | 'later' | 'return' | 'ask' | 'done' | 'answer' | 'submit'

const DONE: Record<Verb, string> = {
  approve: 'Approved',
  decline: 'Declined',
  skip: 'Skipped',
  later: 'Put off',
  return: 'Brought back',
  ask: 'Sent to agent',
  done: 'Marked done',
  answer: 'Answered',
  submit: 'Submitted',
}

const TO: Record<Verb, string> = {
  approve: 'approve',
  decline: 'decline',
  skip: 'skip',
  later: 'put off',
  return: 'bring back',
  ask: 'send to agent',
  done: 'mark done',
  answer: 'answer',
  submit: 'submit',
}

// toastText says what was done, in one line: "Declined: <title>", or "Approved 3 items". applied is
// how many vault changes ran.
export function toastText(verb: Verb, titles: string[], applied = 0): string {
  const base =
    titles.length === 1
      ? `${DONE[verb]}: ${titles[0]}`
      : `${DONE[verb]} ${titles.length} ${titles.length === 1 ? 'item' : 'items'}`
  return applied > 0 ? `${base} — ${applied} ${applied === 1 ? 'change' : 'changes'} applied` : base
}

// failText says an action failed and the card is back.
export function failText(verb: Verb, titles: string[], why: string): string {
  const what = titles.length === 1 ? titles[0] : `${titles.length} items`
  return `Could not ${TO[verb]} ${what}: ${why}`
}

// canUndo: every decision names how the server takes it back (Decision.undo). One that does not
// leaves the action with no Undo.
export const canUndo = (decs: { undo?: string | null }[]): boolean =>
  decs.length > 0 && decs.every((d) => !!d.undo)

// appliedCount is how many decisions ran a vault change.
export const appliedCount = (decs: { undo?: string | null }[]): number =>
  decs.filter((d) => d.undo === 'receipts').length

// whyNoUndo is the line under a toast that has no Undo.
export function whyNoUndo(verb: Verb): string {
  switch (verb) {
    case 'done':
      return 'Done by you cannot be undone here.'
    case 'answer':
      return 'An answer cannot be undone here.'
    default:
      return 'This cannot be undone: it has run, or an agent has it.'
  }
}

type SubmitItem = { item: string; state: string; undo?: string | null }

export type SubmitToast = {
  text: string
  tone?: 'error'
  // decided are the items to undo; empty when nothing was decided.
  decided: SubmitItem[]
  // note is the line under the toast when there is no Undo; none when nothing ran.
  note?: string
}

// submitToast says what a dossier submit did. Skips are recorded but decide nothing, so they are not
// counted. When nothing was decided it says so, with the reasons the server gave, as an error
// and with no undo line: nothing ran and no agent has anything.
export function submitToast(
  r: { items: SubmitItem[]; left?: { item: string; why: string }[] | null },
  title: (id: string) => string,
): SubmitToast {
  const decided = r.items.filter((x) => x.state !== 'pending' && x.state !== 'skipped')
  const left = r.left ?? []
  if (decided.length === 0) {
    const reasons = [
      ...r.items
        .filter((x) => x.state === 'skipped')
        .map((x) => `${title(x.item)}: skipped, not in this batch`),
      ...left.map((x) => `${title(x.item)}: ${x.why}`),
    ]
    return {
      text: `Nothing was submitted: ${reasons.length ? reasons.join('; ') : 'no item has an answer that decides it'}.`,
      tone: 'error',
      decided: [],
    }
  }
  const text = toastText(
    'submit',
    decided.map((x) => title(x.item)),
    appliedCount(decided),
  )
  return {
    text: left.length ? `${text}; ${left.length} not decided` : text,
    decided,
    note: canUndo(decided) ? undefined : whyNoUndo('submit'),
  }
}

export type UndoBody = {
  items?: { proposal: string; item: string }[]
  question?: string
  requests?: string[]
}

// undoBodies are the stack_undo calls that take an action back: one for the dossier items, one per
// vault question. requests are the agent work an ask queued: undo cancels it.
export function undoBodies(
  items: { proposal: string; item: string }[],
  questions: { id: string; request?: string | null }[],
  itemRequests: (string | null | undefined)[] = [],
): UndoBody[] {
  const out: UndoBody[] = []
  const reqs = itemRequests.filter((r): r is string => !!r)
  if (items.length > 0) out.push({ items, requests: reqs.length ? reqs : undefined })
  for (const q of questions) {
    out.push({ question: q.id, requests: q.request ? [q.request] : undefined })
  }
  return out
}

// undoAll makes each call in turn and stops at the first that fails.
export async function undoAll(bodies: UndoBody[], call: (b: UndoBody) => Promise<unknown>) {
  for (const b of bodies) await call(b)
}

// nextFocus is the card that takes focus when the cards in removed leave: the next one in order, or
// the one before when the last leaves, or null when none is left.
export function nextFocus(order: string[], removed: Set<string>): string | null {
  const at = order.findIndex((k) => removed.has(k))
  if (at < 0) return null
  const after = order.slice(at + 1).find((k) => !removed.has(k))
  if (after) return after
  return (
    order
      .slice(0, at)
      .reverse()
      .find((k) => !removed.has(k)) ?? null
  )
}
