import Box from '@mui/material/Box'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import { localDate } from '@/api/today'
import { daysAgo } from '@/lib/when'

// ItemDates is when an item was created and last updated, compact and muted: "created 3 days ago
// · updated today". The date itself shows on hover. An item never changed has no updated date.
export function ItemDates({
  created,
  updated,
  today = localDate(),
}: {
  created?: string | null
  updated?: string | null
  today?: string
}) {
  const parts: { word: string; date: string }[] = []
  if (created) parts.push({ word: 'created', date: created })
  if (updated) parts.push({ word: 'updated', date: updated })
  if (!parts.length) return null
  return (
    <Typography
      component="span"
      variant="caption"
      color="text.secondary"
      data-item-dates
      sx={{ whiteSpace: 'nowrap' }}
    >
      {parts.map((p, i) => (
        <Box component="span" key={p.word}>
          {i > 0 && ' · '}
          <Tooltip title={p.date}>
            <span data-item-date={p.word}>
              {p.word} {daysAgo(p.date, today)}
            </span>
          </Tooltip>
        </Box>
      ))}
    </Typography>
  )
}
