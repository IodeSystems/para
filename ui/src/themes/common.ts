import type { PaletteOptions, Theme } from '@mui/material/styles'

// Rules every proposed direction keeps: a visible focus ring for the keyboard, and 44 px touch
// targets on a phone. A container focused by script (tabindex -1: a dialog, a drawer) gets no
// ring; what the keyboard reaches inside it does. `body` in each selector puts it above a
// component's own single class. An MUI text input is skipped: its outlined/filled field already
// marks focus, and a second ring around it reads as a double border.
export function a11yGlobals(theme: Theme) {
  const v = theme.vars ?? theme
  return {
    'body *:focus-visible:not([tabindex="-1"]):not(.MuiInputBase-input)': {
      outline: `2px solid ${v.palette.primary.main}`,
      outlineOffset: 2,
    },
    [theme.breakpoints.down('sm')]: {
      'body .MuiButton-root, body .MuiToggleButton-root, body .MuiListItemButton-root, body .MuiTab-root':
        { minHeight: 44 },
      'body .MuiIconButton-root': { minHeight: 44, minWidth: 44 },
    },
  }
}

// searchBox styles the OmniBar box in a light app bar. Its own colours (primary when opened on a
// phone, translucent white at a desk) assume a primary-coloured bar.
export function searchBox(theme: Theme, radius: number | string) {
  const v = theme.vars ?? theme
  return {
    '& .MuiToolbar-root > .MuiBox-root:has(> .MuiInputBase-root)': {
      backgroundColor: v.palette.background.paper,
      border: `1px solid ${v.palette.divider}`,
      borderRadius: radius,
    },
  }
}

// Schemes are the palettes of a direction, light and dark. Each file exports its own, so the
// contrast test reads the colours a screen gets (contrast.test.ts).
export type Schemes = Record<'light' | 'dark', PaletteOptions>
