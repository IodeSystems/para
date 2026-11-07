import { createTheme } from '@mui/material/styles'
import { a11yGlobals, searchBox, type Schemes } from './common'

// Direction B, Console: a dense tool. Compact type and spacing, 1 px borders and no shadows, mono
// for ids, counts, dates, and chips. More rows on one screen for keyboard work at a desk; on a
// phone the touch targets still grow to 44 px.
const sans = '"Inter Variable", system-ui, -apple-system, "Segoe UI", sans-serif'
const mono = '"JetBrains Mono Variable", ui-monospace, "SF Mono", Menlo, Consolas, monospace'

export const schemes: Schemes = {
  light: {
    contrastThreshold: 4.5,
    primary: { main: '#1d4ed8' },
    secondary: { main: '#1d4ed8' },
    success: { main: '#15803d' },
    warning: { main: '#a15c07' },
    error: { main: '#b91c1c' },
    info: { main: '#0369a1' },
    background: { default: '#f3f4f6', paper: '#ffffff' },
    text: { primary: '#111827', secondary: '#4b5563' },
    divider: '#d1d5db',
  },
  dark: {
    contrastThreshold: 4.5,
    primary: { main: '#7aa2ff' },
    secondary: { main: '#7aa2ff' },
    success: { main: '#4ade80' },
    warning: { main: '#facc15' },
    error: { main: '#f87171' },
    info: { main: '#38bdf8' },
    background: { default: '#0b0f14', paper: '#121821' },
    text: { primary: '#e5e7eb', secondary: '#9ca3af' },
    divider: '#273142',
  },
}

export const consoleTheme = createTheme({
  cssVariables: { colorSchemeSelector: 'media' },
  colorSchemes: { light: { palette: schemes.light }, dark: { palette: schemes.dark } },
  shape: { borderRadius: 3 },
  spacing: 6,
  typography: {
    fontFamily: sans,
    fontSize: 13.5,
    h5: { fontSize: '1.3rem', fontWeight: 600, letterSpacing: '-0.015em' },
    h6: { fontSize: '1.05rem', fontWeight: 600 },
    subtitle1: { fontSize: '0.95rem', fontWeight: 600 },
    subtitle2: { fontWeight: 600 },
    overline: { fontFamily: mono, fontSize: '0.7rem', letterSpacing: '0.04em', lineHeight: 1.8 },
    caption: { fontFamily: mono, fontSize: '0.72rem' },
    button: { textTransform: 'none', fontWeight: 600, letterSpacing: 0 },
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: (theme) => ({
        ...a11yGlobals(theme),
        'code, kbd, pre': { fontFamily: mono },
      }),
    },
    MuiButton: {
      defaultProps: { disableElevation: true, size: 'small' },
      styleOverrides: { root: { minWidth: 0 } },
    },
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
            ...searchBox(theme, 3),
            '--AppBar-background': v.palette.background.paper,
            '--AppBar-color': v.palette.text.primary,
            backgroundColor: v.palette.background.paper,
            color: v.palette.text.primary,
            borderBottom: `1px solid ${v.palette.divider}`,
            '& .MuiToolbar-root': { minHeight: 40 },
            '& .MuiToolbar-root > .MuiTypography-h6': {
              fontFamily: mono,
              fontSize: '0.95rem',
              fontWeight: 700,
            },
          }
        },
      },
    },
    MuiTabs: {
      styleOverrides: {
        root: ({ theme }) => ({
          minHeight: 36,
          borderTop: `1px solid ${(theme.vars ?? theme).palette.divider}`,
        }),
        indicator: { height: 2 },
      },
    },
    MuiTab: {
      styleOverrides: {
        root: ({ theme }) => ({
          textTransform: 'none',
          fontSize: '0.8rem',
          fontWeight: 600,
          [theme.breakpoints.up('sm')]: { minHeight: 36 },
          paddingTop: 6,
          paddingBottom: 6,
          color: (theme.vars ?? theme).palette.text.secondary,
          '&.Mui-selected': { color: (theme.vars ?? theme).palette.primary.main },
        }),
      },
    },
    MuiChip: {
      styleOverrides: {
        root: { borderRadius: 3, fontFamily: mono },
        sizeSmall: {
          height: 20,
          fontSize: '0.7rem',
          '& .MuiChip-label': { paddingLeft: 6, paddingRight: 6 },
        },
      },
    },
    MuiListItem: { styleOverrides: { root: { paddingTop: 2, paddingBottom: 2 } } },
    MuiListItemText: { styleOverrides: { root: { marginTop: 2, marginBottom: 2 } } },
    MuiBottomNavigation: {
      styleOverrides: {
        root: ({ theme }) => ({
          height: 52,
          borderTop: `1px solid ${(theme.vars ?? theme).palette.divider}`,
        }),
      },
    },
    MuiBottomNavigationAction: {
      styleOverrides: { label: { fontSize: '0.7rem', '&.Mui-selected': { fontSize: '0.7rem' } } },
    },
    MuiTooltip: { styleOverrides: { tooltip: { fontFamily: mono, fontSize: '0.7rem' } } },
  },
})
