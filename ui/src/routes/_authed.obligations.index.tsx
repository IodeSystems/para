import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import Box from '@mui/material/Box'
import Chip from '@mui/material/Chip'
import FormControlLabel from '@mui/material/FormControlLabel'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import Switch from '@mui/material/Switch'
import Tab from '@mui/material/Tab'
import Tabs from '@mui/material/Tabs'
import Typography from '@mui/material/Typography'
import { useObligationRows, useObligationTimeline, type ObligationListRow } from '@/api/obligations'
import { CHIP_ROW, Empty, PageTitle, Section, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { LinkButton, TextLink } from '@/components/links'
import { AgentLooking } from '@/components/FindContext'
import { ChangeButtons } from '@/components/ObligationChange'
import { NewRecurringButton } from '@/components/RecurringArtifact'
import { meetingWhen } from '@/lib/meeting'
import {
  FILTERS,
  hoursWords,
  matchesFilter,
  parseFilter,
  pointWhen,
  promiseWords,
  scoreWords,
  type ObligationFilter,
} from '@/lib/obligations'

export const Route = createFileRoute('/_authed/obligations/')({
  validateSearch: (s: Record<string, unknown>): { filter?: ObligationFilter } => ({
    filter: parseFilter(s.filter),
  }),
  component: Obligations,
})

const TIMELINE_DAYS = 14

function Obligations() {
  const { filter = 'all' } = Route.useSearch()
  const navigate = Route.useNavigate()
  const [all, setAll] = useState(false)
  const rows = useObligationRows(all)
  const timeline = useObligationTimeline(TIMELINE_DAYS)
  return (
    <>
      <PageTitle
        subtitle="What you promised people: meetings you said you would attend, things you committed to, and recurring artifacts an agent prepares for you to approve. Push one out, redirect it, or cancel it, and the people it concerns are told."
        action={
          <Stack direction="row" spacing={1}>
            <NewRecurringButton />
            <LinkButton size="small" to="/agenda">
              Agenda
            </LinkButton>
          </Stack>
        }
      >
        Obligations
      </PageTitle>
      {timeline.data && (
        <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }} data-promise-score>
          {scoreWords(timeline.data.score)}
        </Typography>
      )}
      <Tabs
        value={filter}
        onChange={(_e, v: ObligationFilter) =>
          void navigate({ search: { filter: v === 'all' ? undefined : v }, replace: true })
        }
        variant="scrollable"
        sx={{ mb: 1 }}
        aria-label="Filter"
      >
        {FILTERS.map((f) => (
          <Tab key={f.value} value={f.value} label={f.label} data-filter={f.value} />
        ))}
      </Tabs>
      <Section title={`Next ${TIMELINE_DAYS} days`}>
        <DataState q={timeline}>{(t) => <Timeline t={t} filter={filter} />}</DataState>
      </Section>
      <FormControlLabel
        sx={{ mb: 1 }}
        control={<Switch checked={all} onChange={(e) => setAll(e.target.checked)} />}
        label="Show closed"
      />
      <DataState q={rows}>
        {(list) => {
          const shown = list.filter((r) => matchesFilter(r.obligation, filter))
          return (
            <Section
              title={FILTERS.find((f) => f.value === filter)?.label ?? 'All'}
              count={shown.length}
            >
              {shown.length === 0 ? (
                <Empty>Nothing here.</Empty>
              ) : (
                <List disablePadding data-owed-list>
                  {shown.map((r) => (
                    <Row key={r.id} r={r} />
                  ))}
                </List>
              )}
            </Section>
          )
        }}
      </DataState>
    </>
  )
}

function Row({ r }: { r: ObligationListRow }) {
  const o = r.obligation
  const when =
    o?.kind === 'meeting' && o.start
      ? meetingWhen(o.start, o.end ?? '')
      : o?.date
        ? `due ${o.date}`
        : ''
  const promise = promiseWords(o?.promise)
  return (
    <ListItem disableGutters divider data-obligation-row={r.id} sx={CHIP_ROW}>
      <ListItemText
        primary={
          <TextLink to="/obligations/$id" params={{ id: r.id }} underline="hover">
            {r.title || r.id}
          </TextLink>
        }
        secondary={[
          when,
          o &&
            o.parties.length > 0 &&
            `${o.kind === 'meeting' ? 'with' : 'to'} ${o.parties.join(', ')}`,
          r.life !== 'open' && r.life,
          r.reason,
        ]
          .filter(Boolean)
          .join(' - ')}
        slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
      />
      <AgentLooking id={r.id} />
      {promise && <StatusChip tone={o?.promise === 'broken' ? 'bad' : 'good'} label={promise} />}
      {r.life !== 'closed' && o && !o.agent && o.owedBy.length === 0 && (
        <ChangeButtons x={{ id: r.id, title: r.title, meeting: o.kind === 'meeting' }} />
      )}
    </ListItem>
  )
}

type TimelineData = NonNullable<ReturnType<typeof useObligationTimeline>['data']>
type Point = TimelineData['overdue'][number]

// Timeline lists the coming days: each day's fixed points (meetings at their time, promises at
// their due date) with their open prep. At risk: the open prep takes more working time than is left
// before it, or some prep is blocked.
function Timeline({ t, filter }: { t: TimelineData; filter: ObligationFilter }) {
  const keep = (p: Point) =>
    filter === 'all' ||
    (filter === 'meetings'
      ? p.kind === 'meeting'
      : filter === 'promises'
        ? p.kind !== 'meeting'
        : false)
  const days = t.days
    .map((d) => ({ ...d, points: d.points.filter(keep) }))
    .filter((d) => d.points.length > 0)
  const overdue = t.overdue.filter(keep)
  if (days.length === 0 && overdue.length === 0)
    return <Empty>Nothing fixed in the next days.</Empty>
  return (
    <Stack spacing={1.5} data-timeline>
      {overdue.length > 0 && (
        <Box>
          <Typography variant="subtitle2" color="error">
            Past its date
          </Typography>
          {overdue.map((p) => (
            <TimelinePoint key={p.id} p={p} />
          ))}
        </Box>
      )}
      {days.map((d) => (
        <Box key={d.date} data-day={d.date}>
          <Typography variant="subtitle2">
            {d.weekday} {d.date}
            {d.hours === 0 && ' (no working hours)'}
          </Typography>
          {d.points.map((p) => (
            <TimelinePoint key={p.id} p={p} />
          ))}
        </Box>
      ))}
    </Stack>
  )
}

function TimelinePoint({ p }: { p: Point }) {
  return (
    <Box
      sx={{ pl: 1, py: 0.5, borderLeft: 2, borderColor: p.atRisk ? 'error.main' : 'divider' }}
      data-point={p.id}
    >
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
        <Typography variant="body2" color="text.secondary">
          {pointWhen(p)}
        </Typography>
        <TextLink to="/obligations/$id" params={{ id: p.id }} underline="hover">
          {p.title || p.id}
        </TextLink>
        {p.parties.length > 0 && (
          <Typography variant="caption" color="text.secondary">
            {p.kind === 'meeting' ? 'with' : 'to'} {p.parties.join(', ')}
          </Typography>
        )}
        {p.atRisk && (
          <Chip size="small" color="error" label="At risk" title={p.risk ?? ''} data-at-risk />
        )}
        {p.prep.length > 0 && (
          <Typography variant="caption" color="text.secondary">
            prep {hoursWords(p.prepHours)}, {hoursWords(p.hoursLeft)} of work time left
          </Typography>
        )}
      </Stack>
      {p.prep.length > 0 && (
        <List dense disablePadding sx={{ pl: 2 }}>
          {p.prep.map((x) => (
            <ListItem key={x.id} disableGutters data-prep={x.id}>
              <ListItemText
                primary={<DocLink id={x.id}>{x.title || x.id}</DocLink>}
                secondary={[x.status, x.effort, x.back && `by ${x.back}`, x.blocked && 'blocked']
                  .filter(Boolean)
                  .join(' - ')}
              />
            </ListItem>
          ))}
        </List>
      )}
    </Box>
  )
}
