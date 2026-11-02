import { createFileRoute } from '@tanstack/react-router'
import Chip from '@mui/material/Chip'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Tab from '@mui/material/Tab'
import Tabs from '@mui/material/Tabs'
import Typography from '@mui/material/Typography'
import Alert from '@mui/material/Alert'
import { useGraph, useWork } from '@/api/work'
import { Empty, PageTitle, Section, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { Mermaid } from '@/components/Mermaid'
import { graphToMermaid } from '@/lib/markdown'

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
        <Tab value="tasks" label="Projects and tasks" />
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
  const tasks = w.tasks
  const byProject = new Map<string, typeof tasks>()
  for (const t of tasks) byProject.set(t.project, [...(byProject.get(t.project) ?? []), t])
  const loose = byProject.get('') ?? []
  return (
    <>
      {w.projects.length === 0 && <Empty>No projects yet.</Empty>}
      {w.projects.map((p) => (
        <Section
          key={p.id}
          title={p.title || p.id}
          count={byProject.get(p.id)?.length ?? 0}
          action={
            <Chip
              size="small"
              label={`${p.status}${p.owner ? ` - ${p.owner}` : ''}`}
              variant="outlined"
            />
          }
        >
          <TaskList tasks={byProject.get(p.id) ?? []} />
        </Section>
      ))}
      {loose.length > 0 && (
        <Section title="No project" count={loose.length}>
          <TaskList tasks={loose} />
        </Section>
      )}
    </>
  )
}

function TaskList({ tasks }: { tasks: WorkData['tasks'] }) {
  if (tasks.length === 0) return <Empty>No tasks.</Empty>
  return (
    <List dense disablePadding>
      {tasks.map((t) => (
        <ListItem key={t.id} disableGutters>
          <ListItemText
            primary={<DocLink id={t.id}>{t.title || t.id}</DocLink>}
            secondary={
              t.blockedBy.length
                ? `blocked by ${t.blockedBy.join(', ')}`
                : t.dependsOn.length
                  ? `after ${t.dependsOn.join(', ')}`
                  : undefined
            }
          />
          <StateChip state={t.status} />
        </ListItem>
      ))}
    </List>
  )
}

function GraphView({ project }: { project: string | undefined }) {
  const q = useGraph(project)
  return (
    <DataState q={q}>
      {(g) => {
        const cycles = Array.isArray(g.cycles) ? (g.cycles as string[][]) : []
        return (
          <>
            {cycles.length > 0 && (
              <Alert severity="warning" sx={{ mb: 2 }}>
                {cycles.length} dependency cycle{cycles.length === 1 ? '' : 's'}:{' '}
                {cycles.map((c) => c.join(' > ')).join('; ')}
              </Alert>
            )}
            {g.nodes.length === 0 ? (
              <Empty>No tasks or requests to draw.</Empty>
            ) : (
              <>
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
