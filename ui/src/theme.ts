import type { Theme } from '@mui/material/styles'
import { current } from '@/themes/current'
import { ledger } from '@/themes/ledger'
import { consoleTheme } from '@/themes/console'
import { focus } from '@/themes/focus'
import { DEFAULT_DIRECTION, loadFonts, pickDirection, type Direction } from '@/themes/pick'

// The theme is read once at startup. Focus is the theme of the app; the others stay behind
// ?theme= for one release (themes/pick.ts).
export const THEMES: Record<Direction, Theme> = { current, a: ledger, b: consoleTheme, c: focus }

const direction =
  typeof window === 'undefined' ? DEFAULT_DIRECTION : pickDirection(window.location.search)
loadFonts(direction)

export const theme = THEMES[direction]

// defaultMode is the mode before the owner picks one. Focus opens dark; the themes behind ?theme=
// follow the system setting until the header switch picks light or dark.
export const defaultMode = direction === 'c' ? 'dark' : 'system'

// MODE_KEY is where the light or dark choice is kept in this browser. MUI reads and writes it,
// each access in a try/catch; public/boot.js reads it before the first paint.
export const MODE_KEY = 'para.mode'
