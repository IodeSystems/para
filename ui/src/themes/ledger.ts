import { createTheme } from '@mui/material/styles'
import { a11yGlobals, searchBox, type Schemes } from './common'

// Direction A, Ledger: calm editorial. Serif headings on paper tones, wide spacing, thin rules
// instead of shadows. For reading an item's history and context at a desk.
const serif = '"Source Serif 4 Variable", "Iowan Old Style", Charter, Georgia, serif'
const sans = '"Source Sans 3 Variable", system-ui, -apple-system, "Segoe UI", sans-serif'

export const schemes: Schemes = {
  light: {
    contrastThreshold: 4.5,
    primary: { main: '#8c2f1b' },
    secondary: { main: '#8c2f1b' },
    success: { main: '#2f6b3a' },
    warning: { main: '#875400' },
    error: { main: '#a3271b' },
    info: { main: '#2b5a7a' },
    background: { default: '#f5f1e8', paper: '#fffdf8' },
    text: { primary: '#1f1c17', secondary: '#5a544a' },
    divider: 'rgba(60, 45, 20, 0.16)',
  },
  dark: {
    contrastThreshold: 4.5,
    primary: { main: '#f0a58a' },
    secondary: { main: '#f0a58a' },
    success: { main: '#8fcf9a' },
    warning: { main: '#e8b85c' },
    error: { main: '#f19a8f' },
    info: { main: '#8cc0e0' },
    background: { default: '#171512', paper: '#1f1c18' },
    text: { primary: '#ede7dc', secondary: '#b5ac9c' },
    divider: 'rgba(237, 231, 220, 0.14)',
  },
}

export const ledger = createTheme({
  cssVariables: { colorSchemeSelector: 'data-mui-color-scheme' },
  colorSchemes: { light: { palette: schemes.light }, dark: { palette: schemes.dark } },
  shape: { borderRadius: 2 },
  spacing: 9,
  typography: {
    fontFamily: sans,
    fontSize: 15.5,
    body1: { lineHeight: 1.6 },
    body2: { lineHeight: 1.55 },
    h4: { fontFamily: serif, fontWeight: 600 },
    h5: { fontFamily: serif, fontWeight: 600, letterSpacing: '-0.01em' },
    h6: { fontFamily: serif, fontWeight: 600 },
    subtitle1: { fontFamily: serif, fontWeight: 600, fontSize: '1.15rem' },
    subtitle2: { fontFamily: serif, fontWeight: 600 },
    overline: { letterSpacing: '0.12em', fontWeight: 600 },
    button: { textTransform: 'none', fontWeight: 600, letterSpacing: 0 },
  },
  components: {
    MuiCssBaseline: { styleOverrides: (theme) => a11yGlobals(theme) },
    MuiButton: { defaultProps: { disableElevation: true } },
    MuiPaper: {
      defaultProps: { elevation: 0 },
      styleOverrides: { root: { backgroundImage: 'none' } },
    },
    MuiAppBar: {
      defaultProps: { elevation: 0 },
      styleOverrides: {
        root: ({ theme }) => {
          const v = theme.vars ?? theme
          return {
            ...searchBox(theme, 2),
            '--AppBar-background': v.palette.background.default,
            '--AppBar-color': v.palette.text.primary,
            backgroundColor: v.palette.background.default,
            color: v.palette.text.primary,
            borderBottom: `1px solid ${v.palette.divider}`,
            '& .MuiToolbar-root > .MuiTypography-h6': { fontSize: '1.35rem', fontStyle: 'italic' },
          }
        },
      },
    },
    MuiTab: {
      styleOverrides: {
        root: ({ theme }) => ({
          textTransform: 'none',
          fontSize: '0.95rem',
          fontWeight: 500,
          color: (theme.vars ?? theme).palette.text.secondary,
          '&.Mui-selected': { color: (theme.vars ?? theme).palette.text.primary },
        }),
      },
    },
    MuiTabs: { styleOverrides: { indicator: { height: 2 } } },
    MuiChip: {
      styleOverrides: {
        root: { borderRadius: 2 },
        sizeSmall: { fontSize: '0.75rem', letterSpacing: '0.02em' },
      },
    },
    MuiBottomNavigation: {
      styleOverrides: {
        root: ({ theme }) => ({
          borderTop: `1px solid ${(theme.vars ?? theme).palette.divider}`,
          backgroundColor: (theme.vars ?? theme).palette.background.default,
        }),
      },
    },
    MuiListItem: { styleOverrides: { root: { paddingTop: 6, paddingBottom: 6 } } },
  },
})
