// Which visual direction to show. Focus (c) is the theme of the app. For one release the earlier
// look and the other proposals stay behind the address, with no control in the app: open any page
// with ?theme=current, a (Ledger), or b (Console), and the choice is kept in this browser;
// ?theme=c goes back to Focus.
export const DIRECTIONS = ['current', 'a', 'b', 'c'] as const
export type Direction = (typeof DIRECTIONS)[number]

export const DEFAULT_DIRECTION: Direction = 'c'

const KEY = 'para.theme'

const isDirection = (s: string | null): s is Direction =>
  s !== null && (DIRECTIONS as readonly string[]).includes(s)

export function pickDirection(search: string): Direction {
  const asked = new URLSearchParams(search).get('theme')
  try {
    if (isDirection(asked)) {
      if (asked === DEFAULT_DIRECTION) localStorage.removeItem(KEY)
      else localStorage.setItem(KEY, asked)
      return asked
    }
    const kept = localStorage.getItem(KEY)
    return isDirection(kept) ? kept : DEFAULT_DIRECTION
  } catch {
    // No storage (private mode or blocked): the address alone decides.
    return isDirection(asked) ? asked : DEFAULT_DIRECTION
  }
}

// loadFonts fetches the self-hosted fonts of a direction. A direction that is not picked
// downloads nothing.
export function loadFonts(d: Direction) {
  if (d === 'current') {
    void import('@fontsource/roboto/300.css')
    void import('@fontsource/roboto/400.css')
    void import('@fontsource/roboto/500.css')
    void import('@fontsource/roboto/700.css')
  } else if (d === 'a') {
    void import('@fontsource-variable/source-serif-4')
    void import('@fontsource-variable/source-sans-3')
  } else if (d === 'b') {
    void import('@fontsource-variable/inter')
    void import('@fontsource-variable/jetbrains-mono')
  } else {
    void import('@fontsource-variable/figtree')
  }
}
