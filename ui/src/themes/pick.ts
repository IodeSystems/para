// Which visual direction to show. A preview switch only, with no control in the app: open any page
// with ?theme=a (Ledger), b (Console), c (Focus), or current, and the choice is kept in this
// browser. With neither, the current theme is used.
export const DIRECTIONS = ['current', 'a', 'b', 'c'] as const
export type Direction = (typeof DIRECTIONS)[number]

const KEY = 'para.theme'

const isDirection = (s: string | null): s is Direction =>
  s !== null && (DIRECTIONS as readonly string[]).includes(s)

export function pickDirection(search: string): Direction {
  const asked = new URLSearchParams(search).get('theme')
  try {
    if (isDirection(asked)) {
      if (asked === 'current') localStorage.removeItem(KEY)
      else localStorage.setItem(KEY, asked)
      return asked
    }
    const kept = localStorage.getItem(KEY)
    return isDirection(kept) ? kept : 'current'
  } catch {
    // No storage (private mode or blocked): the address alone decides.
    return isDirection(asked) ? asked : 'current'
  }
}

// loadFonts fetches the self-hosted fonts of a direction. The current theme uses Roboto, which
// main.tsx loads. A direction that is not picked downloads nothing.
export function loadFonts(d: Direction) {
  if (d === 'a') {
    void import('@fontsource-variable/source-serif-4')
    void import('@fontsource-variable/source-sans-3')
  } else if (d === 'b') {
    void import('@fontsource-variable/inter')
    void import('@fontsource-variable/jetbrains-mono')
  } else if (d === 'c') {
    void import('@fontsource-variable/figtree')
  }
}
