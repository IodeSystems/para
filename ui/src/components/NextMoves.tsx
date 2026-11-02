import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import FormControlLabel from '@mui/material/FormControlLabel'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { REASONS, reasonLabel, useRfxPathAdd, useRfxPaths } from '@/api/paths'
import { Empty, Section } from '@/components/Bits'
import { errorText } from '@/client/gqlClient'
import { plainState } from '@/lib/status'

type Move = {
  kind: string
  label: string
  to?: string | null
  toName?: string | null
  medium?: string | null
  value?: string | null
  why: string
  contacts?: number | null
  last?: string | null
  hold?: string | null
  command?: string | null
}

// NextMoves is the path log of an Ask and the next moves when it stalls. A move with a person and a
// medium becomes a new target with a reason. Para sends nothing.
export function NextMoves({ id }: { id: string }) {
  const q = useRfxPaths(id)
  const [move, setMove] = useState<Move | null>(null)
  if (!q.data) return null
  const p = q.data
  return (
    <Section title="Next moves" count={p.moves.length}>
      {p.stalled ? (
        <Alert severity="warning" sx={{ mb: 1 }}>
          Stalled: {p.stalledWhy}.
        </Alert>
      ) : (
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
          Not stalled. These moves are still open to you.
        </Typography>
      )}
      <Typography variant="subtitle2">Tried</Typography>
      {p.log.length === 0 ? (
        <Empty>Nobody yet.</Empty>
      ) : (
        <List dense disablePadding>
          {p.log.map((e, i) => (
            <ListItem key={`${e.to}-${e.medium}-${i}`} disableGutters>
              <ListItemText
                primary={`${e.toName || e.to} by ${e.medium}, ${plainState(e.state)}${
                  e.reason ? `, ${reasonLabel(e.reason)}` : ''
                }`}
                secondary={[
                  e.sent && `sent ${e.sent}`,
                  `asked ${e.contacts} time${e.contacts === 1 ? '' : 's'} in all`,
                  e.last && `last ${e.last}`,
                  e.why,
                ]
                  .filter(Boolean)
                  .join(' - ')}
              />
            </ListItem>
          ))}
        </List>
      )}
      <Typography variant="subtitle2" sx={{ mt: 1 }}>
        Try next
      </Typography>
      <List dense disablePadding>
        {p.moves.map((m, i) => (
          <ListItem
            key={`${m.kind}-${m.to ?? ''}-${m.medium ?? ''}-${i}`}
            disableGutters
            secondaryAction={
              m.to && m.medium ? (
                <Button size="small" onClick={() => setMove(m)}>
                  Take
                </Button>
              ) : undefined
            }
          >
            <ListItemText
              primary={m.label}
              secondary={
                m.hold
                  ? `Held back: ${m.hold}`
                  : [m.why, m.contacts ? `asked ${m.contacts} times, last ${m.last}` : '']
                      .filter(Boolean)
                      .join(' - ')
              }
              slotProps={{ secondary: { color: m.hold ? 'warning.main' : undefined } }}
            />
          </ListItem>
        ))}
      </List>
      {move && <TakeDialog askId={id} move={move} onClose={() => setMove(null)} />}
    </Section>
  )
}

function TakeDialog({ askId, move, onClose }: { askId: string; move: Move; onClose: () => void }) {
  const add = useRfxPathAdd()
  const [reason, setReason] = useState('no_response')
  const [why, setWhy] = useState('')
  const [override, setOverride] = useState(false)
  const held = !!move.hold
  const ready = why.trim() !== '' && (!held || override)
  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="xs" aria-labelledby="take-title">
      <DialogTitle id="take-title">{move.label}</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ mt: 1 }}>
          <Typography variant="body2" color="text.secondary">
            This adds a target to the ask. Para sends nothing: you send it.
          </Typography>
          <TextField
            select
            label="Reason"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          >
            {REASONS.map((r) => (
              <MenuItem key={r.code} value={r.code}>
                {r.label}
              </MenuItem>
            ))}
          </TextField>
          <TextField
            label="Why, in one line"
            value={why}
            onChange={(e) => setWhy(e.target.value)}
            autoFocus
          />
          {held && (
            <FormControlLabel
              control={<Checkbox checked={override} onChange={(_, v) => setOverride(v)} />}
              label={`Held back: ${move.hold}. Ask anyway.`}
            />
          )}
          {add.isError && <Alert severity="error">{errorText(add.error)}</Alert>}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button
          variant="contained"
          disabled={!ready || add.isPending}
          onClick={() =>
            add.mutate(
              {
                id: askId,
                to: move.to ?? '',
                medium: move.medium ?? '',
                value: move.value,
                reason,
                why: why.trim(),
                override,
              },
              { onSuccess: onClose },
            )
          }
        >
          Add to ask
        </Button>
      </DialogActions>
    </Dialog>
  )
}
