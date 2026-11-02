import type { ReactNode } from 'react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import LinearProgress from '@mui/material/LinearProgress'
import Typography from '@mui/material/Typography'
import { errorText } from '@/client/gqlClient'

type Q<T> = {
  data: T | undefined
  error: unknown
  isPending: boolean
  isFetching: boolean
  refetch: () => unknown
}

// SearchState shows search results without ever showing only a spinner. While a query runs, the
// previous results stay on screen, dimmed, under a thin progress bar. With no earlier results the
// page says what it is doing in words. A failed query keeps the old results and says so.
export function SearchState<T>({
  q,
  text,
  typing,
  children,
}: {
  q: Q<T>
  text: string
  // typing is true while the box holds text the URL does not yet: a new query is about to start.
  typing: boolean
  children: (data: T) => ReactNode
}) {
  const busy = q.isFetching || typing
  const retry = (
    <Button color="inherit" size="small" onClick={() => void q.refetch()}>
      Retry
    </Button>
  )
  return (
    <Box aria-busy={busy}>
      {busy && <LinearProgress aria-label="Searching" sx={{ mb: 1 }} />}
      {q.data === undefined && q.isPending && (
        <Typography variant="body2" color="text.secondary" role="status">
          Searching for “{text}”…
        </Typography>
      )}
      {q.data === undefined && !q.isPending && (
        <Alert severity="error" action={retry}>
          {errorText(q.error)}
        </Alert>
      )}
      {q.data !== undefined && (
        <>
          {!!q.error && (
            <Alert severity="warning" action={retry} sx={{ mb: 1 }}>
              The search failed, so these are the last results: {errorText(q.error)}
            </Alert>
          )}
          <Box sx={{ opacity: busy ? 0.55 : 1, transition: 'opacity 120ms' }}>
            {children(q.data)}
          </Box>
        </>
      )}
    </Box>
  )
}
