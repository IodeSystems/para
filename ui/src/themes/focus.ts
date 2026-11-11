import { createTheme, type Theme } from '@mui/material/styles'
import { a11yGlobals, searchBox, type Schemes } from './common'

// Focus, the theme of the app: one strong accent on soft warm surfaces, round shapes, and a large
// Next card. The thing to do now is the biggest thing on the screen; on a phone it reads at arm's
// length and every target is a thumb's size. Dark is the default; the owner switches in the
// header (AppShell ModeSwitch).
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
  // Dark: near-black with a little warmth, not brown. The surfaces step up in lightness, so a
  // card reads above the page and the composer above the card.
  dark: {
    contrastThreshold: 4.5,
    primary: { main: accentDark },
    secondary: { main: accentDark },
    success: { main: '#7fd8a2' },
    warning: { main: '#f2c25b' },
    error: { main: '#ff8a7a' },
    info: { main: '#8ab8ff' },
    background: { default: '#141210', paper: '#1f1b18' },
    text: { primary: '#f6f0ea', secondary: '#bdb1a6' },
    divider: 'rgba(255, 240, 225, 0.11)',
  },
}

// raised is the surface above a card: the composer, menus, dialogs, toasts, and the phone's
// bottom bar. The contrast test holds text on it to AA too.
export const raised = { light: '#ffffff', dark: '#2a2521' }

const shadow = {
  light: '0 1px 2px rgba(60, 30, 10, 0.06), 0 4px 16px rgba(60, 30, 10, 0.05)',
  dark: '0 1px 2px rgba(0, 0, 0, 0.4), 0 6px 20px rgba(0, 0, 0, 0.25)',
}
export const lift = {
  light: '0 2px 6px rgba(60, 30, 10, 0.08), 0 12px 36px rgba(60, 30, 10, 0.14)',
  dark: '0 0 0 1px rgba(255, 240, 225, 0.05), 0 12px 40px rgba(0, 0, 0, 0.55)',
}

// floating styles a surface that sits above the page: menus, popovers, dialogs.
const floating = (theme: Theme) => ({
  backgroundColor: raised.light,
  boxShadow: lift.light,
  border: `1px solid ${(theme.vars ?? theme).palette.divider}`,
  ...theme.applyStyles('dark', { backgroundColor: raised.dark, boxShadow: lift.dark }),
})

export const focus = createTheme({
  // The page starts dark. MUI keeps the owner's choice in localStorage and sets
  // data-mui-color-scheme on <html>; public/boot.js sets it before the first paint.
  cssVariables: { colorSchemeSelector: 'data-mui-color-scheme' },
  defaultColorScheme: 'dark',
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
    MuiCssBaseline: {
      styleOverrides: (theme) => ({
        ...a11yGlobals(theme),
        body: { WebkitFontSmoothing: 'antialiased', MozOsxFontSmoothing: 'grayscale' },
        // A list that stands on the page, not in a section card, gets the card's surface; an
        // empty state there is a quiet dashed box.
        'main > .MuiList-root': {
          backgroundColor: (theme.vars ?? theme).palette.background.paper,
          border: `1px solid ${(theme.vars ?? theme).palette.divider}`,
          borderRadius: 16,
          padding: '4px 16px',
          boxShadow: shadow.light,
          ...theme.applyStyles('dark', { boxShadow: shadow.dark }),
          '& > .MuiListItem-root:last-of-type': { borderBottom: 0 },
        },
        // A toast is a raised surface; its accent edge (ToastStack) stays.
        '[data-toast] .MuiPaper-root': {
          borderRadius: 14,
          backgroundColor: raised.light,
          boxShadow: lift.light,
          ...theme.applyStyles('dark', { backgroundColor: raised.dark, boxShadow: lift.dark }),
        },
        'main > [data-empty]': {
          border: `1px dashed ${(theme.vars ?? theme).palette.divider}`,
          borderRadius: 16,
          padding: theme.spacing(3),
          textAlign: 'center',
        },
        '::selection': {
          backgroundColor: `rgba(${(theme.vars ?? theme).palette.primary.mainChannel} / 0.3)`,
        },
      }),
    },
    MuiButton: {
      defaultProps: { disableElevation: true },
      styleOverrides: {
        root: { borderRadius: 999, minWidth: 0 },
        // On a phone a small button reads at body size.
        sizeSmall: ({ theme }) => ({
          fontSize: '0.85rem',
          [theme.breakpoints.down('sm')]: { fontSize: '1rem' },
        }),
        contained: { paddingLeft: 16, paddingRight: 16 },
        outlined: { paddingLeft: 16, paddingRight: 16 },
      },
    },
    MuiToggleButton: {
      styleOverrides: {
        root: {
          textTransform: 'none',
          fontWeight: 600,
          paddingLeft: 14,
          paddingRight: 14,
          '&:first-of-type': { borderTopLeftRadius: 999, borderBottomLeftRadius: 999 },
          '&:last-of-type': { borderTopRightRadius: 999, borderBottomRightRadius: 999 },
        },
      },
    },
    MuiPaper: {
      styleOverrides: {
        root: ({ theme }) => ({
          backgroundImage: 'none',
          // The composer is a step above the card it sits in.
          '&[data-composer]': {
            backgroundColor: raised.light,
            ...theme.applyStyles('dark', { backgroundColor: raised.dark }),
          },
          // The phone's bottom bar (AppShell) is a raised sheet with round top corners.
          '&[data-bottom-bar]': {
            borderTopLeftRadius: 20,
            borderTopRightRadius: 20,
            overflow: 'hidden',
            backgroundColor: raised.light,
            boxShadow: lift.light,
            ...theme.applyStyles('dark', { backgroundColor: raised.dark, boxShadow: lift.dark }),
          },
        }),
      },
      variants: [
        {
          props: { variant: 'outlined' },
          style: ({ theme }) => {
            const v = theme.vars ?? theme
            return {
              borderColor: v.palette.divider,
              borderRadius: 16,
              boxShadow: shadow.light,
              ...theme.applyStyles('dark', { boxShadow: shadow.dark }),
              // The Next card is the one thing to do now: larger, with the accent on top.
              '&[data-next-card], &[data-next-empty]': {
                borderLeftWidth: 1,
                borderLeftColor: v.palette.divider,
                borderTop: `6px solid ${v.palette.primary.main}`,
                borderRadius: 24,
                padding: theme.spacing(4),
                // A phone keeps the width for the words: the card runs edge to edge, its words
                // on the page's 8 px gutter, and its composer is a full-width bar with no inset
                // of its own beyond that gutter.
                [theme.breakpoints.down('sm')]: {
                  marginLeft: theme.spacing(-1),
                  marginRight: theme.spacing(-1),
                  padding: theme.spacing(1.5, 1),
                  borderLeftWidth: 0,
                  borderRightWidth: 0,
                  borderBottomWidth: 0,
                  borderRadius: 0,
                  boxShadow: 'none',
                  ...theme.applyStyles('dark', { boxShadow: 'none' }),
                  '& [data-item-composer]': {
                    marginLeft: theme.spacing(-1),
                    marginRight: theme.spacing(-1),
                  },
                  '& [data-composer]': {
                    borderLeftWidth: 0,
                    borderRightWidth: 0,
                    borderRadius: '16px 16px 0 0',
                  },
                },
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
    MuiMenu: {
      styleOverrides: { paper: ({ theme }) => ({ ...floating(theme), borderRadius: 14 }) },
    },
    MuiPopover: {
      styleOverrides: { paper: ({ theme }) => ({ ...floating(theme), borderRadius: 16 }) },
    },
    MuiMenuItem: {
      styleOverrides: { root: { borderRadius: 8, marginLeft: 6, marginRight: 6 } },
    },
    MuiDialog: {
      styleOverrides: { paper: ({ theme }) => ({ ...floating(theme), borderRadius: 24 }) },
    },
    MuiDialogTitle: { styleOverrides: { root: { fontWeight: 700 } } },
    MuiTooltip: {
      styleOverrides: {
        tooltip: { borderRadius: 8, fontSize: '0.8rem', fontWeight: 500, padding: '6px 10px' },
      },
    },
    MuiAlert: {
      styleOverrides: {
        root: ({ theme }) => ({
          borderRadius: 14,
          border: `1px solid ${(theme.vars ?? theme).palette.divider}`,
        }),
      },
    },
    MuiChip: {
      styleOverrides: {
        root: { fontWeight: 600 },
        // A quiet chip has a soft border; a status chip keeps its tone's.
        outlined: ({ theme, ownerState }) =>
          !ownerState.color || ownerState.color === 'default'
            ? { borderColor: (theme.vars ?? theme).palette.divider }
            : {},
      },
    },
    MuiOutlinedInput: { styleOverrides: { root: { borderRadius: 12 } } },
    MuiListItemButton: { styleOverrides: { root: { borderRadius: 12 } } },
    MuiListSubheader: {
      styleOverrides: {
        root: ({ theme }) => ({
          backgroundColor: 'transparent',
          color: (theme.vars ?? theme).palette.text.secondary,
          fontSize: '0.75rem',
          fontWeight: 700,
          letterSpacing: '0.08em',
          textTransform: 'uppercase',
        }),
      },
    },
    MuiTableCell: {
      styleOverrides: {
        root: ({ theme }) => ({ borderBottomColor: (theme.vars ?? theme).palette.divider }),
        head: ({ theme }) => ({
          fontWeight: 700,
          color: (theme.vars ?? theme).palette.text.secondary,
          fontSize: '0.8rem',
        }),
      },
    },
    MuiLink: { defaultProps: { underline: 'hover' } },
    MuiAppBar: {
      defaultProps: { elevation: 0 },
      styleOverrides: {
        root: ({ theme }) => {
          const v = theme.vars ?? theme
          return {
            ...searchBox(theme, 999),
            '--AppBar-background': v.palette.background.default,
            '--AppBar-color': v.palette.text.primary,
            backgroundColor: `rgba(${v.palette.background.defaultChannel} / 0.86)`,
            backdropFilter: 'saturate(1.4) blur(14px)',
            color: v.palette.text.primary,
            borderBottom: `1px solid ${v.palette.divider}`,
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
            '&:hover': { color: v.palette.text.primary },
            '&.Mui-selected': {
              color: v.palette.primary.contrastText,
              backgroundColor: v.palette.primary.main,
            },
          }
        },
      },
    },
    MuiBottomNavigation: {
      styleOverrides: { root: { height: 64, backgroundColor: 'transparent' } },
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
