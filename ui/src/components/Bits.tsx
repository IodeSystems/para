import type { ReactNode } from 'react'
import Box from '@mui/material/Box'
import Chip from '@mui/material/Chip'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { PinButton } from '@/components/PinButton'
import { plainState, toneOf, type Tone } from '@/lib/status'

// Small shared pieces of layout.

// CHIP_ROW is for a list row with text and a status chip: on a phone the chip goes under the
// text, on its own line, so it is never clipped or squeezed.
// On a phone the text takes a full line, then the chips and actions share one line under it, and
// the second line of text is cut to one line, so a row is at most three lines.
export const CHIP_ROW = {
  flexWrap: { xs: 'wrap', sm: 'nowrap' },
  alignItems: 'center',
  gap: 0.5,
  '& > .MuiListItemText-root': {
    flexBasis: { xs: '100%', sm: 'auto' },
    '& .MuiListItemText-secondary': {
      overflow: { xs: 'hidden', sm: 'visible' },
      textOverflow: { xs: 'ellipsis', sm: 'clip' },
      whiteSpace: { xs: 'nowrap', sm: 'normal' },
    },
  },
} as const

export function Section({
  title,
  count,
  action,
  hideEmpty,
  children,
}: {
  title: string
  count?: number
  action?: ReactNode
  // hideEmpty drops the whole card when count is 0: an empty card says nothing.
  hideEmpty?: boolean
  children: ReactNode
}) {
  if (hideEmpty && count === 0) return null
  return (
    <Paper component="section" variant="outlined" sx={{ p: 1.5, mb: 2 }}>
      <Stack
        direction="row"
        spacing={1}
        useFlexGap
        sx={{ mb: 1, alignItems: 'center', flexWrap: 'wrap' }}
      >
        <Typography
          variant="subtitle1"
          component="h2"
          sx={{ fontWeight: 600, flexGrow: 1, minWidth: 0, overflowWrap: 'anywhere' }}
        >
          {title}
          {count !== undefined && <Chip size="small" label={count} sx={{ ml: 1 }} />}
        </Typography>
        {action}
      </Stack>
      {children}
    </Paper>
  )
}

// Empty says a list has nothing. data-empty lets a theme draw a page's own empty state (one that
// stands alone on the page, not inside a section) as a quiet box.
export function Empty({ children }: { children: ReactNode }) {
  return (
    <Typography variant="body2" color="text.secondary" data-empty>
      {children}
    </Typography>
  )
}

export function PageTitle({
  children,
  action,
  subtitle,
  pin,
}: {
  children: ReactNode
  action?: ReactNode
  // subtitle says in a line what the screen is for.
  subtitle?: string
  // pin mounts the Pin toggle in the title: the item's id, or a thread key that is no item.
  pin?: { id?: string; key?: string }
}) {
  return (
    <Box sx={{ mb: 2 }}>
      <Stack
        direction={{ xs: 'column', sm: 'row' }}
        spacing={1}
        sx={{ alignItems: { xs: 'flex-start', sm: 'center' } }}
      >
        <Typography variant="h5" component="h1" sx={{ flexGrow: 1, overflowWrap: 'break-word' }}>
          {children}
        </Typography>
        {pin && <PinButton id={pin.id} pinKey={pin.key} size="medium" />}
        {action}
      </Stack>
      {subtitle && (
        <Typography variant="body2" color="text.secondary">
          {subtitle}
        </Typography>
      )}
    </Box>
  )
}

const TONE_COLOR: Record<Tone, 'default' | 'success' | 'warning' | 'error' | 'info'> = {
  good: 'success',
  wait: 'info',
  attn: 'warning',
  bad: 'error',
  quiet: 'default',
}

// The one status chip. Color is the tone (see lib/status.ts), never chosen per screen.
export function StatusChip({ tone, label }: { tone: Tone; label: string }) {
  return <Chip size="small" label={label} color={TONE_COLOR[tone]} variant="outlined" />
}

// StateChip shows a state in words, with the tone the state has everywhere.
export function StateChip({ state, label, tone }: { state: string; label?: string; tone?: Tone }) {
  return <StatusChip tone={tone ?? toneOf(state)} label={label ?? plainState(state)} />
}

// Details hides fields that matter to few people behind a plain "Details" toggle.
export function Details({
  summary = 'Details',
  children,
}: {
  summary?: string
  children: ReactNode
}) {
  return (
    <Box component="details" sx={{ mt: 0.5, fontSize: '0.8125rem', color: 'text.secondary' }}>
      <Box component="summary" sx={{ cursor: 'pointer', width: 'fit-content' }}>
        {summary}
      </Box>
      <Box sx={{ pl: 1.5, pt: 0.25, overflowWrap: 'anywhere' }}>{children}</Box>
    </Box>
  )
}
