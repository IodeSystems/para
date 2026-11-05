import { useState } from 'react'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Menu from '@mui/material/Menu'
import MenuItem from '@mui/material/MenuItem'
import TextField from '@mui/material/TextField'
import { laterDates } from '@/lib/stack'

// LaterButton puts an item off: tomorrow, next week (the next Monday), or a date the owner picks.
// onLater gets the date, YYYY-MM-DD.
export function LaterButton({
  disabled,
  onLater,
}: {
  disabled?: boolean
  onLater: (until: string) => void
}) {
  const [at, setAt] = useState<HTMLElement | null>(null)
  const [picking, setPicking] = useState(false)
  const [date, setDate] = useState('')
  // The dates are of the day the button was drawn.
  const [{ tomorrow, nextWeek }] = useState(() => laterDates(new Date()))
  const pick = (d: string) => {
    setAt(null)
    setPicking(false)
    setDate('')
    onLater(d)
  }
  return (
    <>
      <Button
        size="small"
        data-act="later"
        aria-haspopup="menu"
        aria-expanded={!!at}
        disabled={disabled}
        onClick={(e) => setAt(e.currentTarget)}
      >
        Later
      </Button>
      <Menu anchorEl={at} open={!!at} onClose={() => setAt(null)}>
        <MenuItem data-later="tomorrow" onClick={() => pick(tomorrow)}>
          Tomorrow
        </MenuItem>
        <MenuItem data-later="next-week" onClick={() => pick(nextWeek)}>
          Next week (Monday)
        </MenuItem>
        {picking ? (
          <Box
            sx={{ px: 2, py: 1, display: 'flex', gap: 1 }}
            onKeyDown={(e) => e.stopPropagation()}
          >
            <TextField
              type="date"
              size="small"
              label="Until"
              autoFocus
              value={date}
              slotProps={{ inputLabel: { shrink: true }, htmlInput: { min: tomorrow } }}
              onChange={(e) => setDate(e.target.value)}
            />
            <Button size="small" disabled={date < tomorrow} onClick={() => pick(date)}>
              Put off
            </Button>
          </Box>
        ) : (
          <MenuItem data-later="pick" onClick={() => setPicking(true)}>
            Pick a date
          </MenuItem>
        )}
      </Menu>
    </>
  )
}
