import { createFileRoute } from '@tanstack/react-router'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import Tab from '@mui/material/Tab'
import Tabs from '@mui/material/Tabs'
import Typography from '@mui/material/Typography'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import useMediaQuery from '@mui/material/useMediaQuery'
import { useAreas } from '@/api/areas'
import { useGraph, useWork } from '@/api/work'
import { CHIP_ROW, Empty, PageTitle, Section, StateChip, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { LinkButton, TextLink } from '@/components/links'
import { DocLink } from '@/components/DocLink'
import { QuestionsSection } from '@/components/Questions'
import { TaskRows } from '@/components/TaskRows'
import { RowMenu } from '@/components/RowMenu'
import { AgentLooking, findContextItem } from '@/components/FindContext'
import { useLooking } from '@/api/requests'
import { Mermaid } from '@/components/Mermaid'
import { GRAPH_CLASSES, graphClass, graphToMermaid } from '@/lib/markdown'
import { nameOr } from '@/lib/names'
import { plainState } from '@/lib/status'

type View = 'tasks' | 'graph'

export const Route = createFileRoute('/_authed/work')({
  validateSearch: (s: Record<string, unknown>): { view?: View; project?: string } => ({
    view: s.view === 'graph' ? 'graph' : undefined,
    project: typeof s.project === 'string' && s.project !== '' ? s.project : undefined,
  }),
  component: Work,
})

function Work() {
  const { view, project } = Route.useSearch()
  const navigate = Route.useNavigate()
  const q = useWork()
  return (
    <>
      <PageTitle>Work</PageTitle>
      <Tabs
        value={view ?? 'tasks'}
        onChange={(_e, v: View) =>
          void navigate({
            search: (p) => ({ ...p, view: v === 'graph' ? 'graph' : undefined }),
            replace: true,
          })
        }
        sx={{ mb: 2 }}
      >
        <Tab value="tasks" label="Goals and tasks" />
        <Tab value="graph" label="Graph" />
      </Tabs>
      {view === 'graph' ? (
        <GraphView project={project} />
      ) : (
        <DataState q={q}>{(w) => <Tasks w={w} />}</DataState>
      )}
    </>
  )
}

type WorkData = NonNullable<ReturnType<typeof useWork>['data']>

function Tasks({ w }: { w: WorkData }) {
  const looking = useLooking()
  const tasks = w.tasks
  const byProject = new Map<string, typeof tasks>()
  for (const t of tasks) byProject.set(t.project, [...(byProject.get(t.project) ?? []), t])
  const loose = byProject.get('') ?? []
  const bare = w.projects.filter((p) => (byProject.get(p.id)?.length ?? 0) === 0)
  return (
    <>
      <QuestionsSection hideEmpty />
      <AreasSection />
      {w.projects.length === 0 && <Empty>No goals yet.</Empty>}
      {w.projects
        .filter((p) => (byProject.get(p.id)?.length ?? 0) > 0)
        .map((p) => (
          <Section
            key={p.id}
            title={p.title || p.id}
            count={byProject.get(p.id)?.length ?? 0}
            action={
              <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
                <StateChip state={p.status} />
                <AgentLooking id={p.id} />
                <RowMenu
                  label={`More actions for ${p.title || p.id}`}
                  always
                  items={[findContextItem(p.id, looking.has(p.id))]}
                />
                <LinkButton size="small" to="/projects/$id" params={{ id: p.id }}>
                  Open
                </LinkButton>
              </Stack>
            }
          >
            <TaskRows tasks={byProject.get(p.id) ?? []} />
          </Section>
        ))}
      {bare.length > 0 && (
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          No tasks yet in:{' '}
          {bare.map((p, i) => (
            <span key={p.id}>
              {i > 0 && ', '}
              <TextLink to="/projects/$id" params={{ id: p.id }} underline="hover">
                {p.title || nameOr('', p.id)}
              </TextLink>
            </span>
          ))}
        </Typography>
      )}
      {loose.length > 0 && (
        <Section title="No goal" count={loose.length}>
          <TaskRows tasks={loose} />
        </Section>
      )}
    </>
  )
}

function AreasSection() {
  const q = useAreas()
  const areas = q.data ?? []
  return (
    <Section title="Areas" count={areas.length} hideEmpty>
      {
        <List dense disablePadding>
          {areas.map((a) => (
            <ListItem key={a.id} disableGutters sx={CHIP_ROW}>
              <ListItemText
                primary={
                  <TextLink to="/areas/$id" params={{ id: a.id }} underline="hover">
                    {a.title || a.id}
                  </TextLink>
                }
              />
              <StateChip state={a.status} />
            </ListItem>
          ))}
        </List>
      }
    </Section>
  )
}

const NODE_KIND: Record<string, string> = { question: 'Question', rfx: 'Ask', task: 'Task' }

function GraphLegend() {
  return (
    <Stack
      direction="row"
      useFlexGap
      sx={{ flexWrap: 'wrap', gap: 1.5, mb: 1.5, alignItems: 'center' }}
      aria-label="Legend"
    >
      {Object.values(GRAPH_CLASSES).map((c) => (
        <Stack key={c.label} direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
          <Box
            sx={{
              width: 16,
              height: 12,
              borderRadius: 0.5,
              bgcolor: c.fill,
              border: `2px solid ${c.stroke}`,
            }}
          />
          <Typography variant="caption">{c.label}</Typography>
        </Stack>
      ))}
      <Typography variant="caption" color="text.secondary">
        Box: task. Dashed hexagon: question. Pill: ask to someone.
      </Typography>
    </Stack>
  )
}

type GraphData = NonNullable<ReturnType<typeof useGraph>['data']>

// GraphList is the graph for a narrow screen: one row per item, with what it waits for.
function GraphList({ g }: { g: GraphData }) {
  const byId = new Map(g.nodes.map((n) => [n.id, n]))
  const after = new Map<string, string[]>()
  for (const e of g.edges) {
    const dep = byId.get(e.to)
    after.set(e.from, [...(after.get(e.from) ?? []), dep?.title || nameOr('', e.to)])
  }
  const ordered = [...g.order.map((id) => byId.get(id)), ...g.nodes]
    .filter((n): n is NonNullable<typeof n> => !!n)
    .filter((n, i, all) => all.indexOf(n) === i)
  return (
    <List dense disablePadding>
      {ordered.map((n) => (
        <ListItem key={n.id} disableGutters divider sx={{ display: 'block' }}>
          <Stack direction="row" spacing={1} sx={{ alignItems: 'flex-start' }}>
            <ListItemText
              primary={<DocLink id={n.id}>{n.title || nameOr('', n.id)}</DocLink>}
              secondary={[
                NODE_KIND[n.type] ?? n.type,
                n.projectName,
                after.get(n.id)?.length && `after ${after.get(n.id)?.join(', ')}`,
              ]
                .filter(Boolean)
                .join(' - ')}
              slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
            />
            <StatusChip
              tone={GRAPH_CLASSES[graphClass(n.state, n.status)].tone}
              label={plainState(n.state || n.status)}
            />
          </Stack>
        </ListItem>
      ))}
    </List>
  )
}

function GraphView({ project }: { project: string | undefined }) {
  const q = useGraph(project)
  const narrow = useMediaQuery('(max-width: 719px)')
  return (
    <DataState q={q}>
      {(g) => {
        const titles = new Map(g.nodes.map((n) => [n.id, n.title]))
        const cycles = Array.isArray(g.cycles) ? (g.cycles as string[][]) : []
        return (
          <>
            {cycles.length > 0 && (
              <Alert severity="warning" sx={{ mb: 2 }}>
                {cycles.length} dependency cycle{cycles.length === 1 ? '' : 's'}:{' '}
                {cycles
                  .map((c) => c.map((id) => titles.get(id) || nameOr('', id)).join(' > '))
                  .join('; ')}
              </Alert>
            )}
            {g.nodes.length === 0 ? (
              <Empty>No tasks or asks to draw.</Empty>
            ) : narrow ? (
              <GraphList g={g} />
            ) : (
              <>
                <GraphLegend />
                <Mermaid
                  source={graphToMermaid(g.nodes, g.edges)}
                  label={`Dependency graph of ${g.nodes.length} items`}
                />
                <Typography variant="caption" color="text.secondary">
                  An arrow points from what must finish to what waits on it.
                </Typography>
              </>
            )}
          </>
        )
      }}
    </DataState>
  )
}
