import type { ReactNode } from 'react'
import Chip from '@mui/material/Chip'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'

// Small shared pieces of layout.

export function Section({
  title,
  count,
  action,
  children,
}: {
  title: string
  count?: number
  action?: ReactNode
  children: ReactNode
}) {
  return (
    <Paper component="section" variant="outlined" sx={{ p: 1.5, mb: 2 }}>
      <Stack direction="row" spacing={1} sx={{ mb: 1, alignItems: 'center' }}>
        <Typography variant="subtitle1" component="h2" sx={{ fontWeight: 600, flexGrow: 1 }}>
          {title}
          {count !== undefined && (
            <Chip
              size="small"
              label={count}
              sx={{ ml: 1 }}
              color={count > 0 ? 'primary' : 'default'}
            />
          )}
        </Typography>
        {action}
      </Stack>
      {children}
    </Paper>
  )
}

export function Empty({ children }: { children: ReactNode }) {
  return (
    <Typography variant="body2" color="text.secondary">
      {children}
    </Typography>
  )
}

export function PageTitle({ children, action }: { children: ReactNode; action?: ReactNode }) {
  return (
    <Stack direction="row" spacing={1} sx={{ mb: 2, alignItems: 'center' }}>
      <Typography variant="h5" component="h1" sx={{ flexGrow: 1 }}>
        {children}
      </Typography>
      {action}
    </Stack>
  )
}

const STATE_COLOR: Record<string, 'default' | 'success' | 'warning' | 'error' | 'info'> = {
  replied: 'success',
  done: 'success',
  answered: 'success',
  sent: 'info',
  drafted: 'default',
  no_reply: 'warning',
  blocked: 'error',
  stalled: 'warning',
}

export function StateChip({ state, label }: { state: string; label?: string }) {
  return (
    <Chip
      size="small"
      label={label ?? state.replace(/_/g, ' ')}
      color={STATE_COLOR[state] ?? 'default'}
      variant="outlined"
    />
  )
}
