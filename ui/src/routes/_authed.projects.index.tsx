import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import Box from '@mui/material/Box'
import Stack from '@mui/material/Stack'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Typography from '@mui/material/Typography'
import { type ProjectStatus, useProjects } from '@/api/projects'
import { Empty, PageTitle, Section, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { TextLink } from '@/components/links'
import { StateLine } from '@/components/ProjectBits'
import { nameOr } from '@/lib/names'

// Projects: the ongoing things you keep up, one row each: where it stands, how many goals are in
// flight, what it waits on, and its next cron run. One request; the stalest state line comes first.
export const Route = createFileRoute('/_authed/projects/')({ component: Projects })

const META = { fontSize: '1rem', overflowWrap: 'anywhere' } as const

const STATUSES: { value: ProjectStatus; label: string }[] = [
  { value: 'active', label: 'Active' },
  { value: 'paused', label: 'Paused' },
  { value: 'retired', label: 'Retired' },
  { value: 'all', label: 'All' },
]

function Projects() {
  const [status, setStatus] = useState<ProjectStatus>('active')
  const q = useProjects(status)
  const label = STATUSES.find((s) => s.value === status)?.label ?? 'Active'
  return (
    <>
      <PageTitle subtitle="Ongoing projects: where each stands, and what it waits on. A finite outcome is a goal (Work).">
        Projects
      </PageTitle>
      <DataState q={q}>
        {(rows) => (
          <Section
            title={label}
            count={rows.length}
            action={
              <ToggleButtonGroup
                exclusive
                size="small"
                value={status}
                aria-label="Status"
                onChange={(_, v: ProjectStatus | null) => v && setStatus(v)}
              >
                {STATUSES.map((s) => (
                  <ToggleButton key={s.value} value={s.value} data-project-status={s.value}>
                    {s.label}
                  </ToggleButton>
                ))}
              </ToggleButtonGroup>
            }
          >
            {rows.length === 0 ? (
              <Empty>
                {status === 'all' ? 'No projects.' : `No ${label.toLowerCase()} projects.`}
              </Empty>
            ) : (
              <Stack divider={<Box sx={{ borderTop: 1, borderColor: 'divider' }} />} spacing={1}>
                {rows.map((r) => {
                  const s = r.project
                  const goals = s?.openGoals ?? 0
                  const waits = s?.waitsOn ?? 0
                  return (
                    <Stack key={r.id} spacing={0.25} sx={{ pt: 1 }} data-project={r.id}>
                      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                        <TextLink
                          to="/projects/$id"
                          params={{ id: r.id }}
                          underline="hover"
                          sx={{ fontSize: '1.0625rem', fontWeight: 600, overflowWrap: 'anywhere' }}
                        >
                          {r.title || nameOr('', r.id)}
                        </TextLink>
                        {status === 'all' && r.status && <StateChip state={r.status} />}
                      </Stack>
                      <StateLine line={s?.stateLine} ageDays={s?.stateAgeDays ?? -1} />
                      <Typography color="text.secondary" sx={META}>
                        {goals} open {goals === 1 ? 'goal' : 'goals'}
                        {' - '}
                        {waits > 0
                          ? `waits on ${s?.firstWait ?? ''}${waits > 1 ? ` +${waits - 1}` : ''}`
                          : 'waits on nothing'}
                        {s?.nextCron && ` - next cron ${s.nextCron}`}
                        {!!s?.kpiAlerts &&
                          ` - ${s.kpiAlerts} ${s.kpiAlerts === 1 ? 'KPI' : 'KPIs'} off or stale`}
                        {s?.lastActivity && ` - last activity ${s.lastActivity}`}
                      </Typography>
                    </Stack>
                  )
                })}
              </Stack>
            )}
          </Section>
        )}
      </DataState>
    </>
  )
}
