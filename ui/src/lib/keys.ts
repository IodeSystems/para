// Keyboard shortcuts: digits switch tabs, / searches, ? shows help. On Next, letters act on the one
// card (nextKeyFor).
// No global key opens a form.

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
  { key: '6', to: '/policy', label: 'Policy' },
  { key: '8', to: '/requests', label: 'Asked of agents', hint: 'every piece of agent work' },
  { key: '9', to: '/questions', label: 'Questions' },
]

// Screens with no digit key and no tab: the menu and the Agenda page link to them.
export const EXTRA_SCREENS: Tab[] = [
  { key: '', to: '/io', label: 'IO', hint: 'messages in and out not dealt with yet' },
  { key: '', to: '/docs', label: 'Docs', hint: 'remote documents you follow, and what changed' },
  {
    key: '',
    to: '/sources',
    label: 'Sources',
    hint: 'where work comes in and goes out, and who sweeps it',
  },
  { key: '', to: '/meetings', label: 'Meetings', hint: 'meetings you asked for or booked' },
  { key: '', to: '/activity', label: 'Agent activity', hint: 'what each agent did' },
  { key: '', to: '/friction', label: 'Friction', hint: 'what slows asks down' },
]

export type KeyAction = { kind: 'tab'; to: string } | { kind: 'search' } | { kind: 'help' }

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
  }
  return null
}

export type NextKey = 'next' | 'back' | 'done' | 'later' | 'send' | 'approve' | 'open'

// NEXT_KEYS are the keys of the Next page. s is Send to agent here; g does the same.
export const NEXT_KEYS: { key: string; action: NextKey; words: string }[] = [
  { key: 'j', action: 'next', words: 'Skip to the next card' },
  { key: 'n', action: 'next', words: 'Skip, as j' },
  { key: 'k', action: 'back', words: 'Back to the card you skipped last' },
  { key: 'd', action: 'done', words: 'Done (Drop or Held, as the item allows)' },
  { key: 'l', action: 'later', words: 'Later: put it off to a date' },
  { key: 's', action: 'send', words: 'Send to agent: write what it should do' },
  { key: 'g', action: 'send', words: 'Send to agent, as s' },
  { key: 'a', action: 'approve', words: 'Approve the draft, or accept the proposed answer' },
  { key: 'o', action: 'open', words: "Open the item's page" },
]

// nextKeyFor maps a key press on the Next page to an action, or null.
export function nextKeyFor(e: KeyLike): NextKey | null {
  if (e.ctrlKey || e.metaKey || e.altKey || typing(e.target)) return null
  return NEXT_KEYS.find((k) => k.key === e.key)?.action ?? null
}
