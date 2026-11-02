import { createTheme } from '@mui/material/styles'

// Light and dark from the system setting. CSS variables keep the first paint and the
// `color-scheme` declaration in step with the theme.
export const theme = createTheme({
  cssVariables: { colorSchemeSelector: 'media' },
  colorSchemes: {
    light: { palette: { primary: { main: '#1f3a5f' }, background: { default: '#f6f7f9' } } },
    dark: {
      palette: {
        primary: { main: '#8fb3e8' },
        background: { default: '#10151c', paper: '#171e27' },
      },
    },
  },
  typography: { fontSize: 15 },
  components: {
    MuiButton: { defaultProps: { disableElevation: true } },
  },
})
