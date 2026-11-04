// Keyboard shortcuts: digits switch tabs, / searches, ? shows help. On the review stack, letters
// move and decide (stackKeyFor). No global key opens a form.

export type Tab = { key: string; to: string; label: string; hint?: string }

export const TABS: Tab[] = [
  { key: '1', to: '/agenda', label: 'Agenda', hint: 'what needs you, in value order' },
  {
    key: '2',
    to: '/writes',
    label: 'Recent agent writes',
    hint: 'what agents changed, newest first',
  },
  { key: '3', to: '/work', label: 'Work' },
  { key: '4', to: '/rfx', label: 'Asks', hint: 'asks between you and people' },
  { key: '5', to: '/people', label: 'People' },
  { key: '6', to: '/policy', label: 'Policy' },
  { key: '7', to: '/search', label: 'Search' },
  { key: '8', to: '/requests', label: 'Agent work', hint: 'work queued for an agent' },
  { key: '9', to: '/questions', label: 'Questions' },
  {
    key: '0',
    to: '/stack',
    label: 'Stack',
    hint: 'what agents ask you to decide, across dossiers',
  },
]

// Screens with no digit key and no tab: the menu and the Agenda page link to them.
export const EXTRA_SCREENS: Tab[] = [
  {
    key: '',
    to: '/proposals',
    label: 'Proposals',
    hint: 'the dossiers agents filed for you to review',
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

export type StackKey =
  'down' | 'up' | 'approve' | 'reject' | 'edit' | 'comment' | 'select' | 'approveGroup' | 'open'

// STACK_KEYS are the review stack's keys, for the stack page and the help.
export const STACK_KEYS: { key: string; action: StackKey; words: string }[] = [
  { key: 'j', action: 'down', words: 'Next item' },
  { key: 'k', action: 'up', words: 'Item before' },
  { key: 'a', action: 'approve', words: 'Approve the item (or the selected items)' },
  { key: 'r', action: 'reject', words: 'Reject the item (or the selected items)' },
  { key: 'e', action: 'edit', words: 'Open the item to edit it' },
  { key: 'c', action: 'comment', words: 'Open the item to comment' },
  { key: 'x', action: 'select', words: 'Select the item' },
  { key: 'A', action: 'approveGroup', words: "Approve the item's group" },
  { key: 'o', action: 'open', words: 'Open the dossier' },
]

// stackKeyFor maps a key press on the review stack to an action, or null. Like actionFor, a press
// with a modifier (Shift aside) or inside a text field is never a shortcut.
export function stackKeyFor(e: KeyLike): StackKey | null {
  if (e.ctrlKey || e.metaKey || e.altKey || typing(e.target)) return null
  return STACK_KEYS.find((k) => k.key === e.key)?.action ?? null
}
