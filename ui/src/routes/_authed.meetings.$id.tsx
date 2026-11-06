import { createFileRoute } from '@tanstack/react-router'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useMeeting } from '@/api/meetings'
import { Empty, PageTitle, Section } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink, RfxLink } from '@/components/DocLink'
import { Markdown } from '@/components/Markdown'
import { BriefBody, FileNotesButton } from '@/components/MeetingBrief'
import { MeetingChip } from '@/components/MeetingChip'
import { meetingWhen } from '@/lib/meeting'
import { nameList } from '@/lib/names'
import { MergedFrom, NotesEditor } from '@/components/NotesEditor'

export const Route = createFileRoute('/_authed/meetings/$id')({ component: MeetingPage })

function MeetingPage() {
  const { id } = Route.useParams()
  const q = useMeeting(id)
  return (
    <DataState q={q}>
      {({ meeting: m, body }) => {
        return (
          <>
            <PageTitle>{m.title || id}</PageTitle>
            <Stack direction="row" spacing={1} sx={{ mb: 2, alignItems: 'center' }}>
              <MeetingChip status={m.status} />
              <Typography variant="body2" color="text.secondary">
                {meetingWhen(m.start, m.end)}
              </Typography>
            </Stack>
            <Section title="With" count={m.with.length + m.attendees.length}>
              {m.with.length + m.attendees.length === 0 ? (
                <Empty>Nobody is named yet.</Empty>
              ) : (
                <Typography variant="body2">
                  {[...nameList(m.with, m.withNames), ...m.attendees].join(', ')}
                </Typography>
              )}
            </Section>
            <Section title="Brief">
              <BriefBody id={m.id} />
            </Section>
            <Section title="Asks it should settle" count={m.rfx.length} hideEmpty>
              <List dense disablePadding>
                {m.rfx.map((r, i) => (
                  <ListItem key={r} disableGutters>
                    <ListItemText
                      primary={<RfxLink id={r}>{nameList(m.rfx, m.rfxNames)[i]}</RfxLink>}
                    />
                  </ListItem>
                ))}
              </List>
            </Section>
            <Section title="Calendar events" count={m.calendarUids.length} hideEmpty>
              <List dense disablePadding data-calendar-uids>
                {m.calendarUids.map((e) => (
                  <ListItem key={e.uid} disableGutters>
                    <ListItemText primary={e.uid} secondary={e.role || undefined} />
                  </ListItem>
                ))}
              </List>
            </Section>
            <Section title="Questions and notes">
              <Markdown source={body} />
            </Section>
            <Typography variant="caption" color="text.secondary">
              <DocLink id={m.id}>Open the document</DocLink>
            </Typography>
            <MergedFrom id={id} />
            <NotesEditor id={id} />
            <FileNotesButton id={id} open={m.status === 'requested' || m.status === 'scheduled'} />
          </>
        )
      }}
    </DataState>
  )
}
