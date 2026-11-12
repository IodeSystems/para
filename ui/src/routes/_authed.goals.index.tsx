import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import Box from '@mui/material/Box'
import Stack from '@mui/material/Stack'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Typography from '@mui/material/Typography'
import { type GoalStatus, useGoals } from '@/api/goals'
import { localDate } from '@/api/today'
import { Empty, PageTitle, Section, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { TextLink } from '@/components/links'
import { DueChip } from '@/components/TaskRows'
import { nameOr } from '@/lib/names'

// Goals: the finite outcomes, one row each: due date, project, open tasks, and the next task. One
// request; the earliest due comes first.
export const Route = createFileRoute('/_authed/goals/')({ component: Goals })

const META = { fontSize: '1rem', overflowWrap: 'anywhere' } as const

const STATUSES: { value: GoalStatus; label: string }[] = [
  { value: 'active', label: 'Active' },
  { value: 'paused', label: 'Paused' },
  { value: 'parked', label: 'Parked' },
  { value: 'all', label: 'All' },
]

function Goals() {
  const [status, setStatus] = useState<GoalStatus>('active')
  const q = useGoals(status)
  const label = STATUSES.find((s) => s.value === status)?.label ?? 'Active'
  return (
    <>
      <PageTitle subtitle="Goals: what each is due, how many tasks are open, and what is next.">
        Goals
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
                onChange={(_, v: GoalStatus | null) => v && setStatus(v)}
              >
                {STATUSES.map((s) => (
                  <ToggleButton key={s.value} value={s.value} data-goal-status={s.value}>
                    {s.label}
                  </ToggleButton>
                ))}
              </ToggleButtonGroup>
            }
          >
            {rows.length === 0 ? (
              <Empty>{status === 'all' ? 'No goals.' : `No ${label.toLowerCase()} goals.`}</Empty>
            ) : (
              <Stack divider={<Box sx={{ borderTop: 1, borderColor: 'divider' }} />} spacing={1}>
                {rows.map((r) => {
                  const g = r.goal
                  const open = g?.openTasks ?? 0
                  return (
                    <Stack key={r.id} spacing={0.25} sx={{ pt: 1 }} data-goal={r.id}>
                      <Stack
                        direction="row"
                        spacing={1}
                        sx={{ alignItems: 'center', flexWrap: 'wrap' }}
                        useFlexGap
                      >
                        <TextLink
                          to="/goals/$id"
                          params={{ id: r.id }}
                          underline="hover"
                          sx={{ fontSize: '1.0625rem', fontWeight: 600, overflowWrap: 'anywhere' }}
                        >
                          {r.title || nameOr('', r.id)}
                        </TextLink>
                        {status === 'all' && r.status && <StateChip state={r.status} />}
                        {g?.due && <DueChip due={g.due} today={localDate()} />}
                      </Stack>
                      <Typography color="text.secondary" sx={META}>
                        {g?.project ? (
                          <TextLink to="/projects/$id" params={{ id: g.project }} underline="hover">
                            {nameOr(g.projectTitle, g.project)}
                          </TextLink>
                        ) : (
                          'no project'
                        )}
                        {` - ${open} open ${open === 1 ? 'task' : 'tasks'}`}
                      </Typography>
                      {g?.next && (
                        <Typography sx={META}>
                          Next: <DocLink id={g.next}>{nameOr(g.nextTitle, g.next)}</DocLink>
                        </Typography>
                      )}
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
