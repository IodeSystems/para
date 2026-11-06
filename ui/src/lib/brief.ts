// Pieces of the meeting brief that are plain logic: the groups of one person, and whether the
// agenda opens a row's brief by itself.

export type BriefLine = {
  id: string
  title: string
  type: string
  status?: string | null
  reason: string
}

export type BriefPersonLists = {
  youAsked: BriefLine[]
  theyAsked: BriefLine[]
  tasks: BriefLine[]
  questions: BriefLine[]
}

// personGroups are the non-empty groups of what is open with one person, in the order the brief
// reads them.
export function personGroups(p: BriefPersonLists): { title: string; lines: BriefLine[] }[] {
  return [
    { title: 'You asked them', lines: p.youAsked },
    { title: 'They asked you', lines: p.theyAsked },
    { title: 'Tasks', lines: p.tasks },
    { title: 'Questions', lines: p.questions },
  ].filter((g) => g.lines.length > 0)
}

// lineSecondary is the line under one brief item: why it is listed, then its type and status.
export function lineSecondary(l: BriefLine): string {
  return [l.reason, [l.type, l.status].filter(Boolean).join(', ')].filter(Boolean).join(' - ')
}

// briefOpensItself: on the agenda, a meeting that needs you shows its brief without a click; any
// other meeting row has a Brief button.
export function briefOpensItself(kind: string | null | undefined, needsYou: boolean): boolean {
  return kind === 'meeting' && needsYou
}
