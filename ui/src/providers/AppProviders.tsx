import type { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ThemeProvider } from '@mui/material/styles'
import CssBaseline from '@mui/material/CssBaseline'
import { canSwitchMode, MODE_KEY, theme } from '@/theme'
import { forbidden, httpStatus } from '@/client/gqlClient'

// Polling every 60 s is the fallback when the live socket is down or unsupported. A push only
// invalidates queries; it never carries data.
export const POLL_MS = 60_000

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      refetchInterval: POLL_MS,
      refetchOnWindowFocus: true,
      // A 4xx (401, 403) will not change on a retry.
      retry: (count, err) => {
        const st = httpStatus(err)
        return (
          count < 2 &&
          !(st >= 400 && st < 500) &&
          !forbidden(err) &&
          !(err instanceof Error && /\b4\d\d\b/.test(err.message))
        )
      },
    },
  },
})

export function AppProviders({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      {/* Focus opens dark until the owner switches (AppShell ModeSwitch); the other themes
          follow the system setting. */}
      <ThemeProvider
        theme={theme}
        defaultMode={canSwitchMode ? 'dark' : 'system'}
        modeStorageKey={MODE_KEY}
      >
        <CssBaseline enableColorScheme />
        {children}
      </ThemeProvider>
    </QueryClientProvider>
  )
}
