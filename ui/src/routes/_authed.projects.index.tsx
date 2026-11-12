import { createFileRoute } from '@tanstack/react-router'
import Box from '@mui/material/Box'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useProjects } from '@/api/projects'
import { Empty, PageTitle, Section } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { TextLink } from '@/components/links'
import { nextCron, StateLine } from '@/components/ProjectBits'
import { nameOr } from '@/lib/names'

// Projects: the ongoing things you keep up, one row each: where it stands, how many goals are in
// flight, what it waits on, and its next cron run.
export const Route = createFileRoute('/_authed/projects/')({ component: Projects })

const META = { fontSize: '1rem', overflowWrap: 'anywhere' } as const

function Projects() {
  const q = useProjects()
  return (
    <>
      <PageTitle subtitle="Ongoing projects: where each stands, and what it waits on. A finite outcome is a goal (Work).">
        Projects
      </PageTitle>
      <DataState q={q}>
        {(items) =>
          items.length === 0 ? (
            <Empty>No active projects.</Empty>
          ) : (
            <Section title="Active" count={items.length}>
              <Stack divider={<Box sx={{ borderTop: 1, borderColor: 'divider' }} />} spacing={1}>
                {items.map((d) => {
                  const waits = d.waitsOn.map((w) => w.title || nameOr('', w.id))
                  const cron = nextCron(d.crons)
                  return (
                    <Stack
                      key={d.project.id}
                      spacing={0.25}
                      sx={{ pt: 1 }}
                      data-project={d.project.id}
                    >
                      <TextLink
                        to="/projects/$id"
                        params={{ id: d.project.id }}
                        underline="hover"
                        sx={{ fontSize: '1.0625rem', fontWeight: 600, overflowWrap: 'anywhere' }}
                      >
                        {d.project.title || nameOr('', d.project.id)}
                      </TextLink>
                      <StateLine line={d.project.stateLine} ageDays={d.stateAgeDays} />
                      <Typography color="text.secondary" sx={META}>
                        {d.goals.length} open {d.goals.length === 1 ? 'goal' : 'goals'}
                        {' - '}
                        {waits.length > 0
                          ? `waits on ${waits.slice(0, 2).join(', ')}${waits.length > 2 ? ` +${waits.length - 2}` : ''}`
                          : 'waits on nothing'}
                        {cron && ` - next cron ${cron}`}
                      </Typography>
                    </Stack>
                  )
                })}
              </Stack>
            </Section>
          )
        }
      </DataState>
    </>
  )
}
