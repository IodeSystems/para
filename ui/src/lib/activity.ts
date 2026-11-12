// Helpers for the Recent activity screen: the words of a row, who made it, and where its item is.
import { whoLabel } from '@/lib/history'

type Row = {
  kind: string
  count: number
  who: string
  actor?: string | null
  remote?: boolean | null
  install?: string | null
  doc: string
  docType?: string | null
  docName?: string | null
}

const KINDS: Record<string, string> = {
  created: 'Created',
  edited: 'Edited',
  state: 'State changed',
  closed: 'Closed',
  reopened: 'Reopened',
  moved: 'Moved',
  renamed: 'Renamed',
  sent: 'Sent',
  approved: 'Approved',
  reviewed: 'Reviewed',
  turn: 'Turn posted',
  reply: 'Reply',
  external: 'Edited by hand',
  merged: 'Merged',
  undone: 'Write undone',
}

// ACTIVITY_KINDS lists every kind the server returns, in the order the filter shows them.
export const ACTIVITY_KINDS = Object.keys(KINDS)

// ACTORS are the who filters: the empty value is everyone.
export const ACTORS: { value: string; label: string }[] = [
  { value: '', label: 'Anyone' },
  { value: 'owner', label: 'You' },
  { value: 'agents', label: 'Agents' },
  { value: 'external', label: 'Outside para' },
  { value: 'remote', label: 'Another machine' },
]

// activityKind says the kind of a row in words.
export function activityKind(kind: string): string {
  return KINDS[kind] ?? kind.replace(/_/g, ' ')
}

// activityWho names who made a row; a change from another machine says so.
export function activityWho(r: Pick<Row, 'who' | 'actor' | 'remote' | 'install'>): string {
  const name = whoLabel(r)
  return r.remote ? `${name} on ${r.install || 'another machine'}` : name
}

// activityWhat is the row in a line: the kind, how many changes a burst holds, and the item.
export function activityWhat(r: Row): string {
  const n = r.count > 1 ? ` (${r.count} changes)` : ''
  return `${activityKind(r.kind)}${n}: ${r.docName || r.doc}`
}

// activityTarget is the page of a row's item.
export function activityTarget(r: Pick<Row, 'doc' | 'docType'>): {
  to: '/rfx/$id' | '/io/$id' | '/goals/$id' | '/projects/$id' | '/obligations/$id' | '/docs/$id'
  id: string
} {
  switch (r.docType) {
    case 'rfx':
      return { to: '/rfx/$id', id: r.doc }
    case 'io':
      return { to: '/io/$id', id: r.doc }
    case 'goal':
      return { to: '/goals/$id', id: r.doc }
    case 'project':
      return { to: '/projects/$id', id: r.doc }
    case 'obligation':
      return { to: '/obligations/$id', id: r.doc }
  }
  return { to: '/docs/$id', id: r.doc }
}
