// Words for a priority: what it does to incoming messages. inherited is what a person with no
// priority of their own takes from their groups.
export function priorityHelp(value: string, inherited?: string | null): string {
  switch (value) {
    case 'high':
      return 'Every incoming message from it needs you.'
    case 'low':
    case 'normal':
      return 'Incoming messages go to the digest unless they ask for a reply.'
    default:
      return inherited === 'high'
        ? 'Not set: high from a group.'
        : 'Not set: normal. Messages go to the digest unless they ask for a reply.'
  }
}

type Row = { needsYou: boolean; digest?: boolean | null }

// splitInbox puts the rows that need the owner first, then the rest, each in their order.
export function splitInbox<T extends Row>(rows: T[]): { needsYou: T[]; rest: T[] } {
  return { needsYou: rows.filter((r) => r.needsYou), rest: rows.filter((r) => !r.needsYou) }
}

// digestRows are the open incoming rows the server marks digest: not the owner's turn.
export function digestRows<T extends Row>(rows: T[]): T[] {
  return rows.filter((r) => !!r.digest)
}
