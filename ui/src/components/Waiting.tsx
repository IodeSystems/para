import Alert from '@mui/material/Alert'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import { localDate } from '@/api/today'
import { useWaiting } from '@/api/areas'
import { errorText } from '@/client/gqlClient'
import { Empty, Section, StateChip } from '@/components/Bits'
import { DocLink, RfxLink } from '@/components/DocLink'
import { DueChip } from '@/components/TaskRows'

// What you wait on: requests you sent, and tasks assigned to someone else.
export function WaitingSection() {
  const q = useWaiting(localDate())
  const w = q.data
  return (
    <Section title="Waiting" count={w?.count ?? 0}>
      {q.isError && !w ? (
        <Alert severity="error">{errorText(q.error)}</Alert>
      ) : !w || w.count === 0 ? (
        <Empty>Nobody owes you an answer.</Empty>
      ) : (
        <List dense disablePadding>
          {w.rfx.map((x) => (
            <ListItem key={x.id} disableGutters>
              <ListItemText
                primary={<RfxLink id={x.id}>{x.title || x.id}</RfxLink>}
                secondary={
                  [x.route, x.nextNudge && `nudge ${x.nextNudge}`].filter(Boolean).join(' - ') ||
                  undefined
                }
              />
              <Stack direction="row" spacing={0.5}>
                {x.stalled && <StateChip state="stalled" />}
                {x.due && <StateChip state="blocked" label="nudge due" />}
              </Stack>
            </ListItem>
          ))}
          {w.tasks.map((t) => (
            <ListItem key={t.id} disableGutters>
              <ListItemText
                primary={<DocLink id={t.id}>{t.title || t.id}</DocLink>}
                secondary={t.assignee ? `with ${t.assignee}` : undefined}
              />
              {t.due && <DueChip due={t.due} today={w.today} />}
            </ListItem>
          ))}
        </List>
      )}
    </Section>
  )
}
