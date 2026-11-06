import { createFileRoute } from '@tanstack/react-router'
import Chip from '@mui/material/Chip'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useSeenOnOpen } from '@/api/requests'
import { useMeeting, useObligation } from '@/api/meetings'
import { Empty, PageTitle, Section } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink, RfxLink } from '@/components/DocLink'
import { Markdown } from '@/components/Markdown'
import { BriefBody, FileNotesButton } from '@/components/MeetingBrief'
import { MeetingChip } from '@/components/MeetingChip'
import { meetingWhen } from '@/lib/meeting'
import { nameList } from '@/lib/names'
import { MergedFrom, NotesEditor } from '@/components/NotesEditor'
import { ProduceSection } from '@/components/RecurringArtifact'

export const Route = createFileRoute('/_authed/obligations/$id')({ component: ObligationPage })

// ObligationPage is a meeting's page, or a commitment's: what is owed, to whom, by when. One an
// agent owes shows what it produces, its template, and the last artifact with its state.
function ObligationPage() {
  const { id } = Route.useParams()
  // Opening the item, or the agent work itself, marks its finished agent work seen.
  useSeenOnOpen(id)
  const q = useObligation(id)
  return (
    <DataState q={q}>
      {({ task: o, body, produce }) =>
        o.kind === 'meeting' ? (
          <MeetingDetail id={id} />
        ) : (
          <>
            <PageTitle>{o.title || id}</PageTitle>
            <Stack
              direction="row"
              spacing={1}
              sx={{ mb: 2, alignItems: 'center' }}
              data-obligation={id}
            >
              <Chip size="small" label={o.status === 'todo' ? 'open' : (o.outcome ?? o.status)} />
              <Typography variant="body2" color="text.secondary">
                {[o.due && `due ${o.due}`, o.reason].filter(Boolean).join(' - ')}
              </Typography>
            </Stack>
            <Section title="Owed to" count={o.with.length}>
              {o.with.length === 0 ? (
                <Empty>Nobody is named.</Empty>
              ) : (
                <Typography variant="body2" data-owed-to>
                  {nameList(o.with, o.withNames).join(', ')}
                </Typography>
              )}
            </Section>
            {produce && <ProduceSection id={id} p={produce} />}
            {o.assignee && !produce && (
              <Section title="Owed by">
                <Typography variant="body2">{o.assigneeName || o.assignee}</Typography>
              </Section>
            )}
            {o.doneWhen && (
              <Section title="Done when">
                <Typography variant="body2">{o.doneWhen}</Typography>
              </Section>
            )}
            <Section title="Notes">
              <Markdown source={body} />
            </Section>
            <Typography variant="caption" color="text.secondary">
              <DocLink id={id}>Open the document</DocLink>
            </Typography>
            <MergedFrom id={id} />
            <NotesEditor id={id} />
          </>
        )
      }
    </DataState>
  )
}

function MeetingDetail({ id }: { id: string }) {
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
