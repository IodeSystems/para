import type { Theme } from '@mui/material/styles'
import { current } from '@/themes/current'
import { ledger } from '@/themes/ledger'
import { consoleTheme } from '@/themes/console'
import { focus } from '@/themes/focus'
import { loadFonts, pickDirection, type Direction } from '@/themes/pick'

// The theme is read once at startup. The current theme stays the default; the three proposed
// directions are a preview (themes/pick.ts).
export const THEMES: Record<Direction, Theme> = { current, a: ledger, b: consoleTheme, c: focus }

const direction = typeof window === 'undefined' ? 'current' : pickDirection(window.location.search)
loadFonts(direction)

export const theme = THEMES[direction]
