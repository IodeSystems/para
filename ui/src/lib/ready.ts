// readyWords is the chip of an item an agent prepared, by what was prepared: the server says
// ready (prepared and not changed since) and the kind.
const READY_WORDS: Record<string, string> = {
  draft: 'Ready: draft reply',
  answer: 'Ready: proposed answer',
  fields: 'Ready: filled in',
  brief: 'Ready: brief',
  plan: 'Ready: plan',
}

export function readyWords(kind?: string | null): string {
  return (kind && READY_WORDS[kind]) || 'Ready'
}

// readyTarget is the page that holds what was prepared: the draft, or the item itself when it is
// on the item (a proposed answer, a plan, filled fields).
export function readyTarget(it: { id: string; readyRef?: string | null }): string {
  return it.readyRef || it.id
}
