import type { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ThemeProvider } from '@mui/material/styles'
import CssBaseline from '@mui/material/CssBaseline'
import { theme } from '@/theme'

// Polling every 60 s is the fallback when the live socket is down or unsupported. A push only
// invalidates queries; it never carries data.
export const POLL_MS = 60_000

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      refetchInterval: POLL_MS,
      refetchOnWindowFocus: true,
      retry: (count, err) => count < 2 && !(err instanceof Error && /\b4\d\d\b/.test(err.message)),
    },
  },
})

export function AppProviders({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider theme={theme} defaultMode="system">
        <CssBaseline enableColorScheme />
        {children}
      </ThemeProvider>
    </QueryClientProvider>
  )
}
