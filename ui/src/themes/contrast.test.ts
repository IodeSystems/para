import { describe, expect, it } from 'vitest'
import { createTheme, decomposeColor, getContrastRatio, type Palette } from '@mui/material/styles'
import type { Schemes } from './common'
import { schemes as current } from './current'
import { schemes as ledger } from './ledger'
import { schemes as console_ } from './console'
import { raised, schemes as focus } from './focus'

// WCAG AA for normal text is 4.5:1. A colour with alpha is blended onto the surface first.
const AA = 4.5

function solid(fg: string, bg: string): string {
  const f = decomposeColor(fg)
  const a = f.values[3] ?? 1
  if (a >= 1) return fg
  const b = decomposeColor(bg).values
  const mix = [0, 1, 2].map((i) => Math.round(f.values[i] * a + b[i] * (1 - a)))
  return `rgb(${mix.join(', ')})`
}

const ratio = (fg: string, bg: string) => getContrastRatio(solid(fg, bg), bg)

// palette resolves a scheme as MUI does: defaults filled in and contrastText computed.
const palette = (s: Schemes, mode: 'light' | 'dark'): Palette =>
  createTheme({ palette: { ...s[mode], mode } }).palette

// below lists the text-on-surface pairs under AA: body and secondary text, links in the primary
// colour, outlined status chips, and filled buttons.
function below(p: Palette): string[] {
  const out: [string, string, string][] = []
  for (const [name, bg] of [
    ['default', p.background.default],
    ['paper', p.background.paper],
  ]) {
    out.push([`text.primary on ${name}`, p.text.primary, bg])
    out.push([`text.secondary on ${name}`, p.text.secondary, bg])
    for (const tone of ['primary', 'success', 'warning', 'error', 'info'] as const)
      out.push([`${tone} on ${name}`, p[tone].main, bg])
  }
  for (const tone of ['primary', 'success', 'warning', 'error', 'info'] as const)
    out.push([`${tone}.contrastText on ${tone}`, p[tone].contrastText, p[tone].main])
  return out
    .map(([what, fg, bg]) => ({ what, r: ratio(fg, bg) }))
    .filter(({ r }) => r < AA)
    .map(({ what, r }) => `${what} ${r.toFixed(2)}`)
}

describe('contrast', () => {
  const proposed: Record<string, Schemes> = { ledger, console: console_, focus }
  for (const [name, s] of Object.entries(proposed)) {
    for (const mode of ['light', 'dark'] as const) {
      it(`${name} ${mode} meets AA`, () => {
        expect(below(palette(s, mode))).toEqual([])
      })
    }
  }

  // Focus is the theme of the app. Text also sits on its raised surface: the composer, menus,
  // dialogs, toasts, and the phone's bottom bar.
  for (const mode of ['light', 'dark'] as const) {
    it(`focus ${mode}: text on the raised surface meets AA`, () => {
      const p = palette(focus, mode)
      const bg = raised[mode]
      const out = (
        [
          ['text.primary', p.text.primary],
          ['text.secondary', p.text.secondary],
          ['primary', p.primary.main],
          ['success', p.success.main],
          ['warning', p.warning.main],
          ['error', p.error.main],
          ['info', p.info.main],
        ] as const
      )
        .map(([what, fg]) => ({ what, r: ratio(fg, bg) }))
        .filter(({ r }) => r < AA)
        .map(({ what, r }) => `${what} on raised ${r.toFixed(2)}`)
      expect(out).toEqual([])
    })
  }

  // The current theme is measured, not held to AA: it is what the owner uses today. Its light
  // light scheme has MUI's default warning and info: under AA as chip text and under white text.
  // Its dark scheme has white on MUI's dark error.
  it('current: what is under AA', () => {
    expect(below(palette(current, 'light')).map((s) => s.split(' on ')[0])).toEqual([
      'warning',
      'info',
      'warning',
      'info',
      'warning.contrastText',
      'info.contrastText',
    ])
    expect(below(palette(current, 'dark'))).toEqual(['error.contrastText on error 3.68'])
  })
})
