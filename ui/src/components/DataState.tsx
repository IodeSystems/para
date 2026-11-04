import type { ReactNode } from 'react'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import CircularProgress from '@mui/material/CircularProgress'
import Stack from '@mui/material/Stack'
import { errorText, forbidden } from '@/client/gqlClient'

type Q<T> = { data: T | undefined; error: unknown; isPending: boolean; refetch: () => unknown }

// Shows loading and error states of a query, and the children once there is data. A refetch
// failure keeps the old data on screen.
export function DataState<T>({ q, children }: { q: Q<T>; children: (data: T) => ReactNode }) {
  if (q.data !== undefined) return <>{children(q.data)}</>
  if (q.isPending) {
    return (
      <Stack sx={{ py: 4, alignItems: 'center' }}>
        <CircularProgress aria-label="Loading" />
      </Stack>
    )
  }
  // A refused key is not a fault to retry: it says what key to use.
  if (forbidden(q.error)) return <Alert severity="info">{errorText(q.error)}</Alert>
  return (
    <Alert
      severity="error"
      action={
        <Button color="inherit" size="small" onClick={() => void q.refetch()}>
          Retry
        </Button>
      }
    >
      {errorText(q.error)}
    </Alert>
  )
}
