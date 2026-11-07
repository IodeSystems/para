import { createTheme } from '@mui/material/styles'
import { a11yGlobals, searchBox, type Schemes } from './common'

// Direction C, Focus: one strong accent on soft warm surfaces, round shapes, and a large Next
// card. The thing to do now is the biggest thing on the screen; on a phone it reads at arm's
// length and every target is a thumb's size.
const sans = '"Figtree Variable", system-ui, -apple-system, "Segoe UI", sans-serif'
const accentLight = '#b93c0b'
const accentDark = '#ff9a5c'

export const schemes: Schemes = {
  light: {
    contrastThreshold: 4.5,
    primary: { main: accentLight },
    secondary: { main: accentLight },
    success: { main: '#2f7a4a' },
    warning: { main: '#935700' },
    error: { main: '#b42318' },
    info: { main: '#2563a8' },
    background: { default: '#faf6f1', paper: '#ffffff' },
    text: { primary: '#2a211b', secondary: '#66584d' },
    divider: 'rgba(80, 50, 20, 0.12)',
  },
  dark: {
    contrastThreshold: 4.5,
    primary: { main: accentDark },
    secondary: { main: accentDark },
    success: { main: '#7ed9a0' },
    warning: { main: '#f2c25b' },
    error: { main: '#ff8a7a' },
    info: { main: '#8ab8ff' },
    background: { default: '#1b1613', paper: '#261f1a' },
    text: { primary: '#f4ece4', secondary: '#c2b4a8' },
    divider: 'rgba(244, 236, 228, 0.12)',
  },
}

export const focus = createTheme({
  cssVariables: { colorSchemeSelector: 'media' },
  colorSchemes: { light: { palette: schemes.light }, dark: { palette: schemes.dark } },
  shape: { borderRadius: 12 },
  typography: {
    fontFamily: sans,
    fontSize: 15.5,
    h5: { fontWeight: 700, letterSpacing: '-0.01em' },
    h6: { fontWeight: 700 },
    subtitle1: { fontWeight: 700 },
    subtitle2: { fontWeight: 700 },
    overline: { fontWeight: 700, letterSpacing: '0.08em' },
    button: { textTransform: 'none', fontWeight: 700, letterSpacing: 0 },
  },
  components: {
    MuiCssBaseline: { styleOverrides: (theme) => a11yGlobals(theme) },
    MuiButton: {
      defaultProps: { disableElevation: true },
      styleOverrides: {
        root: { borderRadius: 999, minWidth: 0 },
        sizeSmall: { fontSize: '0.85rem' },
        contained: { paddingLeft: 16, paddingRight: 16 },
        outlined: { paddingLeft: 16, paddingRight: 16 },
      },
    },
    MuiPaper: {
      styleOverrides: { root: { backgroundImage: 'none' } },
      variants: [
        {
          props: { variant: 'outlined' },
          style: ({ theme }) => {
            const v = theme.vars ?? theme
            return {
              borderColor: v.palette.divider,
              boxShadow: '0 1px 2px rgba(60, 30, 10, 0.06), 0 4px 16px rgba(60, 30, 10, 0.05)',
              // The Next card is the one thing to do now: larger, with the accent on top.
              '&[data-next-card], &[data-next-empty]': {
                borderLeftWidth: 1,
                borderLeftColor: v.palette.divider,
                borderTop: `6px solid ${v.palette.primary.main}`,
                borderRadius: 20,
                padding: theme.spacing(2),
                [theme.breakpoints.up('sm')]: { padding: theme.spacing(4) },
                '& > .MuiTypography-overline': { color: v.palette.primary.main },
                '& > h2.MuiTypography-h5': {
                  fontSize: '1.75rem',
                  lineHeight: 1.2,
                  marginTop: 4,
                  marginBottom: 8,
                  [theme.breakpoints.up('sm')]: { fontSize: '2.25rem' },
                },
              },
            }
          },
        },
      ],
    },
    MuiAppBar: {
      defaultProps: { elevation: 0 },
      styleOverrides: {
        root: ({ theme }) => {
          const v = theme.vars ?? theme
          return {
            ...searchBox(theme, 999),
            '--AppBar-background': v.palette.background.default,
            '--AppBar-color': v.palette.text.primary,
            backgroundColor: v.palette.background.default,
            color: v.palette.text.primary,
            '& .MuiToolbar-root > .MuiTypography-h6': {
              fontWeight: 800,
              color: v.palette.primary.main,
              letterSpacing: '-0.02em',
            },
          }
        },
      },
    },
    MuiTabs: {
      styleOverrides: {
        root: { paddingLeft: 8, paddingRight: 8, paddingBottom: 6 },
        indicator: { display: 'none' },
      },
    },
    MuiTab: {
      styleOverrides: {
        root: ({ theme }) => {
          const v = theme.vars ?? theme
          return {
            textTransform: 'none',
            fontWeight: 600,
            borderRadius: 999,
            minHeight: 36,
            marginRight: 2,
            color: v.palette.text.secondary,
            '&.Mui-selected': {
              color: v.palette.primary.contrastText,
              backgroundColor: v.palette.primary.main,
            },
          }
        },
      },
    },
    MuiChip: { styleOverrides: { root: { fontWeight: 600 } } },
    MuiBottomNavigation: {
      styleOverrides: { root: { height: 64, borderTopLeftRadius: 20, borderTopRightRadius: 20 } },
    },
    MuiBottomNavigationAction: {
      styleOverrides: {
        root: ({ theme }) => ({
          '&.Mui-selected .MuiSvgIcon-root': {
            backgroundColor: `rgba(${(theme.vars ?? theme).palette.primary.mainChannel} / 0.16)`,
            borderRadius: 999,
            padding: '2px 14px',
            boxSizing: 'content-box',
          },
        }),
      },
    },
  },
})
