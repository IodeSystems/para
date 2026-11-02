// Keyboard shortcuts: digits switch tabs, / searches, c captures, ? shows help.

export type Tab = { key: string; to: string; label: string; hint?: string }

export const TABS: Tab[] = [
  { key: '1', to: '/agenda', label: 'Agenda', hint: 'what needs you, in value order' },
  { key: '2', to: '/review', label: 'Review' },
  { key: '3', to: '/work', label: 'Work' },
  { key: '4', to: '/rfx', label: 'Asks', hint: 'asks between you and people' },
  { key: '5', to: '/people', label: 'People' },
  { key: '6', to: '/policy', label: 'Policy' },
  { key: '7', to: '/search', label: 'Search' },
  { key: '8', to: '/requests', label: 'Agent work', hint: 'work queued for an agent' },
  { key: '9', to: '/questions', label: 'Questions' },
]

// Screens with no digit key and no tab: the menu and the Agenda page link to them.
export const EXTRA_SCREENS: Tab[] = [
  { key: '', to: '/meetings', label: 'Meetings', hint: 'meetings you asked for or booked' },
  { key: '', to: '/activity', label: 'Agent activity', hint: 'what each agent did' },
]

export type KeyAction =
  { kind: 'tab'; to: string } | { kind: 'search' } | { kind: 'capture' } | { kind: 'help' }

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
    case 'c':
      return { kind: 'capture' }
    case '?':
      return { kind: 'help' }
  }
  return null
}
