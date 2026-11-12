import type { ReactNode } from 'react'
import { createFileRoute, Navigate } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useSeenOnOpen } from '@/api/requests'
import { useProject } from '@/api/projects'
import { localDate } from '@/api/today'
import { CHIP_ROW, Empty, PageTitle, Section, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { AgentLooking } from '@/components/FindContext'
import { ItemLog } from '@/components/ItemLog'
import { TextLink } from '@/components/links'
import { Markdown } from '@/components/Markdown'
import { MergedFrom, NotesEditor } from '@/components/NotesEditor'
import { PointerLinks } from '@/components/PointerLinks'
import { KPIReadingEditor, StateLineEditor } from '@/components/ProjectBits'
import { DueChip } from '@/components/TaskRows'
import { nameOr } from '@/lib/names'

// A project: the bundle the server builds (project_show). The same route opens an old project id:
// when it names a goal (a former id keeps resolving), the page goes to the goal.
export const Route = createFileRoute('/_authed/projects/$id')({ component: ProjectPage })

type ItemRow = {
  id: string
  type: string
  title: string
  status?: string | null
  due?: string | null
  via?: string | null
  viaTitle?: string | null
  note?: string | null
  hiddenTasks?: number | null
}

const TEXT = { fontSize: '1rem' } as const

function Items({ list, empty }: { list: readonly ItemRow[]; empty: string }) {
  if (list.length === 0) return <Empty>{empty}</Empty>
  return (
    <List dense disablePadding>
      {list.map((i) => (
        <ListItem key={i.id} disableGutters sx={CHIP_ROW}>
          <ListItemText
            primary={<DocLink id={i.id}>{i.title || nameOr('', i.id)}</DocLink>}
            secondary={
              [
                i.type,
                i.via && `through ${nameOr(i.viaTitle, i.via)}`,
                i.note,
                i.hiddenTasks ? `${i.hiddenTasks} open tasks hidden while parked` : '',
              ]
                .filter(Boolean)
                .join(' - ') || undefined
            }
            slotProps={{
              primary: { sx: TEXT },
              secondary: { sx: { ...TEXT, overflowWrap: 'anywhere' } },
            }}
          />
          {i.status && <StateChip state={i.status} />}
          {i.due && <DueChip due={i.due} today={localDate()} />}
        </ListItem>
      ))}
    </List>
  )
}

// A cron line names an item as `"<title>" (<id>)`; each becomes a link titled by the title.
const CRON_ITEM = /"([^"]*)" \(([a-z][a-z-]*-[0-9a-z]+)\)/g

function CronLine({ line }: { line: string }) {
  const parts: ReactNode[] = []
  let at = 0
  for (const m of line.matchAll(CRON_ITEM)) {
    parts.push(line.slice(at, m.index))
    parts.push(
      <DocLink key={m.index} id={m[2]}>
        {m[1]}
      </DocLink>,
    )
    at = m.index + m[0].length
  }
  parts.push(line.slice(at))
  return <>{parts}</>
}

// A KPI line has its state: on, off, stale, "off, stale", or no reading (kpi.go), then its owner
// when it has one. Off and stale lines show in the warning color. The owner adds a reading below
// the list (KPIReadingEditor); agents add theirs through kpi_reading_add.
function KPILine({ line }: { line: string }) {
  const alert = /; (off|stale|off, stale)(; owner [^;]*)?$/.test(line)
  return (
    <Typography
      component="span"
      color={alert ? 'warning.main' : undefined}
      sx={{ ...TEXT, overflowWrap: 'anywhere' }}
    >
      {line}
    </Typography>
  )
}

function ProjectPage() {
  const { id } = Route.useParams()
  useSeenOnOpen(id)
  const q = useProject(id)
  return (
    <DataState q={q}>
      {(d) => {
        if (d.__typename === 'para_GoalShowOutput') {
          return <Navigate to="/goals/$id" params={{ id: d.goal.id }} replace />
        }
        if (d.__typename !== 'para_ProjectShowOutput') {
          return <Alert severity="error">{id} is not a project.</Alert>
        }
        const p = d.project
        return (
          <>
            <PageTitle
              pin={{ id: p.id }}
              action={
                <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
                  <AgentLooking id={p.id} />
                  <StateChip state={p.status} />
                </Stack>
              }
            >
              {p.title || p.id}
            </PageTitle>
            <StateLineEditor id={p.id} line={p.stateLine} ageDays={d.stateAgeDays} />
            <Typography color="text.secondary" sx={{ ...TEXT, mt: 1, mb: 2 }}>
              {p.owner ? `owner ${nameOr('', p.owner)}` : 'no owner'}
              {p.area && (
                <>
                  {' - area '}
                  <TextLink to="/areas/$id" params={{ id: p.area }} underline="hover">
                    {nameOr(p.areaName, p.area)}
                  </TextLink>
                </>
              )}
              {p.cadence && ` - review ${p.cadence}`}
              {p.lastReviewed && ` - last reviewed ${p.lastReviewed}`}
              {d.lastActivity && ` - last activity ${d.lastActivity}`}
            </Typography>
            <PointerLinks links={p.links} />
            {d.kpiAlerts.length > 0 && (
              <Typography color="warning.main" sx={{ ...TEXT, mb: 1 }} data-kpi-alerts>
                KPIs: {d.kpiAlerts.join(', ')}
              </Typography>
            )}
            <Section title="KPIs" count={d.kpis.length} hideEmpty>
              <List dense disablePadding>
                {d.kpis.map((k) => (
                  <ListItem key={k} disableGutters>
                    <ListItemText primary={<KPILine line={k} />} />
                  </ListItem>
                ))}
              </List>
              <KPIReadingEditor id={p.id} />
            </Section>
            <Section title="Goals in flight" count={d.goals.length}>
              {d.goals.length === 0 ? (
                <Empty>No goals in flight.</Empty>
              ) : (
                <Stack spacing={1.5}>
                  {d.goals.map((g) => (
                    <Stack key={g.id} spacing={0.25} data-goal={g.id}>
                      <Stack
                        direction="row"
                        spacing={1}
                        sx={{ alignItems: 'center', flexWrap: 'wrap' }}
                        useFlexGap
                      >
                        <TextLink
                          to="/goals/$id"
                          params={{ id: g.id }}
                          underline="hover"
                          sx={{ fontSize: '1.0625rem', fontWeight: 600, overflowWrap: 'anywhere' }}
                        >
                          {g.title || nameOr('', g.id)}
                        </TextLink>
                        {g.status !== 'active' && <StateChip state={g.status} />}
                        {g.due && <DueChip due={g.due} today={localDate()} />}
                      </Stack>
                      {g.doneWhen && (
                        <Typography
                          color="text.secondary"
                          sx={{ ...TEXT, overflowWrap: 'anywhere' }}
                        >
                          Done when: {g.doneWhen}
                        </Typography>
                      )}
                      {g.next && (
                        <Typography sx={{ ...TEXT, overflowWrap: 'anywhere' }}>
                          Next: <DocLink id={g.next}>{nameOr(g.nextTitle, g.next)}</DocLink>
                        </Typography>
                      )}
                      <Items list={g.tasks} empty="No open tasks." />
                      {!!g.moreTasks && (
                        <Typography color="text.secondary" sx={TEXT}>
                          +{g.moreTasks} more open tasks
                        </Typography>
                      )}
                    </Stack>
                  ))}
                </Stack>
              )}
            </Section>
            <Section title="Waits on" count={d.waitsOn.length}>
              <Items list={d.waitsOn} empty="Waits on nothing." />
            </Section>
            <Section title="Icebox" count={d.icebox.length} hideEmpty>
              <Items list={d.icebox} empty="" />
            </Section>
            <Section title="Linked directly" count={d.direct.length} hideEmpty>
              <Items list={d.direct} empty="" />
            </Section>
            <Section title="Crons" count={d.crons.length} hideEmpty>
              <List dense disablePadding>
                {d.crons.map((c) => (
                  <ListItem key={c} disableGutters>
                    <ListItemText
                      primary={<CronLine line={c} />}
                      slotProps={{ primary: { sx: { ...TEXT, overflowWrap: 'anywhere' } } }}
                    />
                  </ListItem>
                ))}
              </List>
            </Section>
            <Section title="History" count={d.history.length} hideEmpty>
              <List dense disablePadding>
                {d.history.map((h) => (
                  <ListItem key={`${h.at}-${h.id}-${h.kind}`} disableGutters>
                    <ListItemText
                      primary={h.summary}
                      secondary={`${h.at} - ${h.kind} - ${nameOr('', h.id)}`}
                      slotProps={{
                        primary: { sx: { ...TEXT, overflowWrap: 'anywhere' } },
                        secondary: { sx: { ...TEXT, overflowWrap: 'anywhere' } },
                      }}
                    />
                  </ListItem>
                ))}
              </List>
            </Section>
            {d.more.length > 0 && (
              <Typography color="text.secondary" sx={{ ...TEXT, mb: 2 }}>
                {d.more.join(' ')}
              </Typography>
            )}
            {d.body.trim() !== '' && (
              <Section title="About">
                <Markdown source={d.body} />
              </Section>
            )}
            <ItemLog id={p.id} composer />
            <MergedFrom id={p.id} />
            <NotesEditor id={p.id} />
          </>
        )
      }}
    </DataState>
  )
}
