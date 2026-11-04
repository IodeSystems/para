import Alert from '@mui/material/Alert'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Typography from '@mui/material/Typography'
import { reasonLabel, useRfxPaths } from '@/api/paths'
import { Empty, Section } from '@/components/Bits'
import { plainState } from '@/lib/status'

const times = (n: number) => (n === 1 ? '1 time' : `${n} times`)

// NextMoves is the path log of an Ask and the next moves when it stalls. The owner takes a move in
// the chat. Para sends nothing.
export function NextMoves({ id }: { id: string }) {
  const q = useRfxPaths(id)
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
                  `asked ${times(e.contacts)} in all`,
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
          <ListItem key={`${m.kind}-${m.to ?? ''}-${m.medium ?? ''}-${i}`} disableGutters>
            <ListItemText
              primary={m.label}
              secondary={
                m.hold
                  ? `Held back: ${m.hold}`
                  : [m.why, m.contacts ? `asked ${times(m.contacts)}, last ${m.last}` : '']
                      .filter(Boolean)
                      .join(' - ')
              }
              slotProps={{ secondary: { color: m.hold ? 'warning.main' : undefined } }}
            />
          </ListItem>
        ))}
      </List>
    </Section>
  )
}
