// Keyboard shortcuts: digits switch tabs, / searches, ? shows help. On Next, letters act on the one
// card (nextKeyFor).
// t opens Tell agent (capture, then the agent files it).

export type Tab = { key: string; to: string; label: string; hint?: string }

export const TABS: Tab[] = [
  { key: '7', to: '/next', label: 'Next', hint: 'the one thing to do now, as a card' },
  { key: '1', to: '/agenda', label: 'Agenda', hint: 'what needs you, in value order' },
  {
    key: '2',
    to: '/receipts',
    label: 'Receipts',
    hint: 'check, verify, or undo what agents wrote',
  },
  { key: '3', to: '/work', label: 'Work' },
  { key: '4', to: '/rfx', label: 'Asks', hint: 'asks between you and people' },
  { key: '5', to: '/people', label: 'People' },
  {
    key: '6',
    to: '/areas',
    label: 'Areas',
    hint: 'areas, the policies they follow, exceptions and conventions',
  },
  { key: '8', to: '/requests', label: 'Asked of agents', hint: 'every piece of agent work' },
  {
    key: '0',
    to: '/obligations',
    label: 'Obligations',
    hint: 'what you promised people: meetings, commitments, and recurring ones, with their prep',
  },
]

// Screens with no digit key and no tab: the menu and the Agenda page link to them.
export const EXTRA_SCREENS: Tab[] = [
  { key: '', to: '/io', label: 'IO', hint: 'messages in and out not dealt with yet' },
  { key: '', to: '/docs', label: 'Docs', hint: 'remote documents you follow, and what changed' },
  {
    key: '',
    to: '/activity',
    label: 'Recent activity',
    hint: 'what changed in the vault, and who changed it',
  },
  {
    key: '',
    to: '/sources',
    label: 'Sources',
    hint: 'where work comes in and goes out, and who sweeps it',
  },
  {
    key: '',
    to: '/pools',
    label: 'Pools',
    hint: 'ideas kept for later, picked when a trigger comes',
  },
]

export type KeyAction =
  { kind: 'tab'; to: string } | { kind: 'search' } | { kind: 'help' } | { kind: 'tell' }

type KeyLike = {
  key: string
  ctrlKey: boolean
  metaKey: boolean
  altKey: boolean
  target: { tagName?: string; isContentEditable?: boolean } | null
}

// typing says whether the key press belongs to a text field.
function typing(t: KeyLike['target']): boolean {
  if (!t) return false
  const tag = (t.tagName ?? '').toUpperCase()
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || t.isContentEditable === true
}

// actionFor maps a key press to an action, or null. A press with a modifier, or inside a text
// field, is never a shortcut.
export function actionFor(e: KeyLike): KeyAction | null {
  if (e.ctrlKey || e.metaKey || e.altKey || typing(e.target)) return null
  const tab = TABS.find((t) => t.key === e.key)
  if (tab) return { kind: 'tab', to: tab.to }
  switch (e.key) {
    case '/':
      return { kind: 'search' }
    case '?':
      return { kind: 'help' }
    case 't':
      return { kind: 'tell' }
  }
  return null
}

export type NextKey =
  'next' | 'back' | 'done' | 'investigate' | 'later' | 'send' | 'approve' | 'open'

// NEXT_KEYS are the keys of the Next page. s sends the card's composer to an agent.
export const NEXT_KEYS: { key: string; action: NextKey; words: string }[] = [
  { key: 'j', action: 'next', words: 'Skip to the next card' },
  { key: 'n', action: 'next', words: 'Skip, as j' },
  { key: 'k', action: 'back', words: 'Back to the card you skipped last' },
  { key: 'd', action: 'done', words: 'Mark done, or Drop where the item allows' },
  {
    key: 'i',
    action: 'investigate',
    words: 'Investigate: an agent fills in details, checks if done, rechecks assumptions',
  },
  { key: 'l', action: 'later', words: 'Later: put it off to a date' },
  { key: 's', action: 'send', words: 'Send to agent: write what it should do' },
  { key: 'a', action: 'approve', words: 'Approve the draft, or accept the proposed answer' },
  { key: 'o', action: 'open', words: "Open the item's page" },
]

// nextKeyFor maps a key press on the Next page to an action, or null.
export function nextKeyFor(e: KeyLike): NextKey | null {
  if (e.ctrlKey || e.metaKey || e.altKey || typing(e.target)) return null
  return NEXT_KEYS.find((k) => k.key === e.key)?.action ?? null
}
