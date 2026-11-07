import { createTheme } from '@mui/material/styles'
import type { Schemes } from './common'

export const schemes: Schemes = {
  light: { primary: { main: '#1f3a5f' }, background: { default: '#f6f7f9' } },
  dark: { primary: { main: '#8fb3e8' }, background: { default: '#10151c', paper: '#171e27' } },
}

// The theme in use today. Light and dark from the system setting. CSS variables keep the first
// paint and the `color-scheme` declaration in step with the theme.
export const current = createTheme({
  cssVariables: { colorSchemeSelector: 'data-mui-color-scheme' },
  colorSchemes: { light: { palette: schemes.light }, dark: { palette: schemes.dark } },
  typography: { fontSize: 15 },
  components: {
    MuiButton: { defaultProps: { disableElevation: true } },
  },
})
