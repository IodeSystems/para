// The omni bar's pure parts: what a typed string means, how hits group, how the keys move.

export type OmniHit = { id: string; title: string; kind: string }

export type OmniEntry = {
  // key is the option's stable id suffix.
  key: string
  group: string
  label: string
  // Exactly one of to (a path) or id (a document) is set.
  to?: string
  id?: string
  hint?: string
}

// PAGES are the screens the bar can go to by name.
export const PAGES: { label: string; to: string; words: string[] }[] = [
  { label: 'Pinned', to: '/pinned', words: ['pinned', 'pins', 'watch', 'home'] },
  { label: 'Next', to: '/next', words: ['next'] },
  { label: 'Today', to: '/agenda', words: ['today'] },
  { label: 'Agenda', to: '/agenda', words: ['agenda'] },
  {
    label: 'Asked of agents',
    to: '/requests',
    words: ['asked', 'sent', 'loop', 'requests', 'agent', 'agent work'],
  },
  { label: 'Work', to: '/work', words: ['work', 'goals'] },
  { label: 'Projects', to: '/projects', words: ['projects', 'ongoing'] },
  { label: 'Goals', to: '/goals', words: ['goals', 'outcomes'] },
  { label: 'Asks', to: '/rfx', words: ['asks', 'rfx'] },
  { label: 'People', to: '/people', words: ['people'] },
  {
    label: 'Areas',
    to: '/areas',
    words: ['areas', 'policy', 'exceptions', 'conventions', 'constraints'],
  },
  { label: 'Questions', to: '/agenda?show=questions', words: ['questions'] },
  { label: 'Log', to: '/log', words: ['log', 'receipts'] },
  {
    label: 'Recent activity',
    to: '/activity',
    words: ['activity', 'recent', 'changes', 'change log'],
  },
  { label: 'Meetings', to: '/meetings', words: ['meetings'] },
  { label: 'Input', to: '/input', words: ['input', 'inbox', 'messages', 'incoming'] },
  { label: 'Output', to: '/output', words: ['output', 'outbox', 'proposals', 'sent', 'drafts'] },
  { label: 'Docs', to: '/docs', words: ['docs', 'tracked'] },
  { label: 'Sources', to: '/sources', words: ['sources'] },
  { label: 'Pools', to: '/pools', words: ['pools', 'ideas', 'icebox'] },
  { label: 'Health', to: '/health', words: ['health'] },
]

const ID =
  /^(task|project|area|person|policy|question|rfx|proposal|request|incident|note|meeting|org|domain|group|team|role|inbox|io|constraint|exception|transcript|goal)-[a-z0-9][a-z0-9_-]*$/

// idOf returns the document id the text is, or null. Only a whole typed id counts.
export function idOf(text: string): string | null {
  const t = text.trim()
  return ID.test(t) ? t : null
}

// pagesFor lists the screens whose name or a keyword starts with the text.
export function pagesFor(text: string): OmniEntry[] {
  const t = text.trim().toLowerCase()
  if (t === '') return []
  return PAGES.filter(
    (p) => p.label.toLowerCase().startsWith(t) || p.words.some((w) => w.startsWith(t)),
  ).map((p) => ({ key: `page-${p.to}`, group: 'Go to', label: p.label, to: p.to }))
}

export const GROUP_ORDER = [
  'Tasks',
  'Projects',
  'Goals',
  'Meetings',
  'Questions',
  'Asks',
  'People',
  'Docs',
]

// groupOf names the group of a hit.
export function groupOf(h: OmniHit): string {
  if (h.kind === 'meeting' || h.id.startsWith('meeting-')) return 'Meetings'
  switch (h.kind) {
    case 'task':
      return 'Tasks'
    case 'project':
      return 'Projects'
    case 'goal':
      return 'Goals'
    case 'question':
      return 'Questions'
    case 'rfx':
      return 'Asks'
    case 'person':
      return 'People'
  }
  return 'Docs'
}

const PER_GROUP = 4

// entriesFor builds the flat, ordered option list: a pasted id first, then page names, then
// hits by group (at most PER_GROUP each; one entry per document).
export function entriesFor(text: string, hits: readonly OmniHit[]): OmniEntry[] {
  const out: OmniEntry[] = []
  const id = idOf(text)
  if (id) out.push({ key: `id-${id}`, group: 'Open', label: id, id })
  out.push(...pagesFor(text))
  const seen = new Set<string>()
  const by = new Map<string, OmniEntry[]>()
  for (const h of hits) {
    if (seen.has(h.id) || h.id === id) continue
    seen.add(h.id)
    const g = groupOf(h)
    const list = by.get(g) ?? []
    if (list.length < PER_GROUP)
      list.push({ key: `hit-${h.id}`, group: g, label: h.title || h.id, id: h.id, hint: h.id })
    by.set(g, list)
  }
  for (const g of GROUP_ORDER) out.push(...(by.get(g) ?? []))
  return out
}

// nextIndex moves the active option. -1 is none; moving past either end wraps through none.
export function nextIndex(cur: number, dir: 'up' | 'down', n: number): number {
  if (n <= 0) return -1
  if (dir === 'down') return cur >= n - 1 ? -1 : cur + 1
  return cur < 0 ? n - 1 : cur - 1
}

// isOmniKey says a key press asks for the bar: Ctrl or Cmd with K.
export function isOmniKey(e: { key: string; ctrlKey: boolean; metaKey: boolean; altKey: boolean }) {
  return (e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === 'k'
}
