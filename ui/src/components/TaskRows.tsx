import { useState } from 'react'
import Button from '@mui/material/Button'
import IconButton from '@mui/material/IconButton'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Menu from '@mui/material/Menu'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import EventIcon from '@mui/icons-material/Event'
import { useNavigate } from '@tanstack/react-router'
import { useQuestions, useUndefer } from '@/api/questions'
import { useWork } from '@/api/work'
import { useTaskDone, useTaskDue } from '@/api/tasks'
import { localDate } from '@/api/today'
import { Empty, StateChip, StatusChip } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'
import { RowMenu } from '@/components/RowMenu'
import { AgentLooking, findContextItem } from '@/components/FindContext'
import { useLooking } from '@/api/requests'
import { dueLabel, dueState } from '@/lib/due'
import { laterLabel, nameList, nameOr } from '@/lib/names'

export type TaskRow = {
  id: string
  title: string
  status: string
  due?: string | null
  recur?: string | null
  assignee?: string | null
  assigneeName?: string | null
  project?: string | null
  projectName?: string | null
  dependsOn: string[]
  dependsOnNames?: string[] | null
  blockedBy: string[]
  doneWhen?: string | null
  deferred?: boolean
  deferUntil?: string | null
  waitsOn?: string[]
  waitsOnNames?: string[] | null
}

// DueChip shows a due date. Overdue is red, due today or soon is amber, later is gray.
export function DueChip({ due, today = localDate() }: { due: string; today?: string }) {
  const s = dueState(due, today)
  return (
    <StatusChip
      tone={s === 'overdue' ? 'bad' : s === 'today' || s === 'soon' ? 'attn' : 'quiet'}
      label={dueLabel(due, today)}
    />
  )
}

// waitingOn says why a task cannot start: an open question first, then a block, then the tasks it
// follows. An answered question is not a reason, so its id is not listed.
function waitingOn(t: TaskRow, open: Map<string, string[]>): string | undefined {
  const qs = open.get(t.id)
  if (qs?.length) return `waiting on question: ${qs.join('; ')}`
  if (t.blockedBy.length) return `blocked by ${t.blockedBy.join(', ')}`
  const after = nameList(t.dependsOn, t.dependsOnNames).filter(
    (_n, i) => !t.dependsOn[i]?.startsWith('question-'),
  )
  return after.length ? `after ${after.join(', ')}` : undefined
}

// showProject adds the project name under each task, for lists that mix projects.
export function TaskRows({ tasks, showProject }: { tasks: TaskRow[]; showProject?: boolean }) {
  const questions = useQuestions({ deferred: true })
  const undefer = useUndefer()
  const looking = useLooking()
  // task id -> titles of the open questions it waits on
  const waits = new Map<string, string[]>()
  for (const q of questions.data ?? [])
    for (const id of q.blocks) waits.set(id, [...(waits.get(id) ?? []), q.title || q.id])
  if (tasks.length === 0) return <Empty>No tasks.</Empty>
  return (
    <List dense disablePadding>
      {tasks.map((t) => {
        const open = t.status !== 'done' && t.status !== 'dropped'
        return (
          <ListItem
            key={t.id}
            disableGutters
            divider
            className="row-with-menu"
            sx={{ display: 'block' }}
          >
            <Stack direction="row" spacing={1} sx={{ alignItems: 'flex-start' }}>
              <ListItemText
                primary={<DocLink id={t.id}>{t.title || t.id}</DocLink>}
                secondary={
                  [
                    showProject && t.project ? nameOr(t.projectName, t.project) : undefined,
                    waitingOn(t, waits),
                    open && t.doneWhen ? `Done when: ${t.doneWhen}` : undefined,
                  ]
                    .filter(Boolean)
                    .join(' - ') || undefined
                }
                slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
              />
              {open && (
                <RowMenu
                  label={`Actions for ${t.title || t.id}`}
                  items={[
                    { label: 'Done', hint: 'Mark this task finished', search: { done: t.id } },
                    findContextItem(t.id, looking.has(t.id)),
                    {
                      label: 'Set done when',
                      hint: 'Say how we know this task is done',
                      search: { taskdw: t.id },
                    },
                    t.deferred
                      ? {
                          label: 'Not later',
                          hint: 'Bring this back to the Agenda and next',
                          onSelect: () => undefer.mutate([t.id]),
                        }
                      : {
                          label: 'Later',
                          hint: 'Put this off. It leaves the Agenda and next',
                          search: { later: t.id },
                        },
                    {
                      label: 'To question',
                      hint: 'Turn this into a question you need answered first',
                      search: { toq: t.id },
                    },
                  ]}
                />
              )}
            </Stack>
            <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', mt: 0.5 }}>
              {t.status !== 'todo' && <StateChip state={t.status} />}
              {t.due && open && <DueChip due={t.due} />}
              {t.deferred && (
                <StatusChip
                  tone="quiet"
                  label={laterLabel(t.deferUntil || (t.waitsOn?.length ? null : 'someday'), t.waitsOn, t.waitsOnNames)}
                />
              )}
              <AgentLooking id={t.id} />
              {t.recur && <StatusChip tone="quiet" label={`repeats ${t.recur}`} />}
              {t.assignee && (
                <StatusChip tone="quiet" label={`with ${nameOr(t.assigneeName, t.assignee)}`} />
              )}
            </Stack>
          </ListItem>
        )
      })}
    </List>
  )
}

// plusDays is the date `n` days after an ISO date, as an ISO date.
function plusDays(date: string, n: number): string {
  const [y, m, d] = date.split('-').map(Number)
  return localDate(new Date(y ?? 1970, (m ?? 1) - 1, (d ?? 1) + n))
}

// TaskInline is Done and a date action on a task row, in place: no dialog. The date menu offers
// today, tomorrow, next week, or any day.
export function TaskInline({ id, title }: { id: string; title: string }) {
  const done = useTaskDone()
  const due = useTaskDue()
  const work = useWork()
  const navigate = useNavigate()
  // A task with a done_when asks first, in the Done dialog that shows the condition.
  const doneWhen = work.data?.tasks.find((t) => t.id === id)?.doneWhen
  const [anchor, setAnchor] = useState<HTMLElement | null>(null)
  const today = localDate()
  const set = (date: string) => {
    setAnchor(null)
    due.mutate({ id, date })
  }
  return (
    <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center', flexShrink: 0 }}>
      <Button
        size="small"
        disabled={done.isPending || work.isPending}
        aria-label={`Done: ${title}`}
        onClick={() =>
          doneWhen
            ? void navigate({ to: '.', search: (p) => ({ ...p, done: id }) })
            : done.mutate({ id })
        }
      >
        Done
      </Button>
      <IconButton
        size="small"
        aria-label={`Change the date of ${title}`}
        aria-haspopup="menu"
        onClick={(e) => setAnchor(e.currentTarget)}
      >
        <EventIcon fontSize="small" />
      </IconButton>
      <Menu anchorEl={anchor} open={!!anchor} onClose={() => setAnchor(null)}>
        <MenuItem onClick={() => set(today)}>Today</MenuItem>
        <MenuItem onClick={() => set(plusDays(today, 1))}>Tomorrow</MenuItem>
        <MenuItem onClick={() => set(plusDays(today, 7))}>In a week</MenuItem>
        <MenuItem disableRipple sx={{ '&:hover': { bgcolor: 'transparent' } }}>
          <TextField
            type="date"
            size="small"
            label="Another day"
            slotProps={{ inputLabel: { shrink: true } }}
            onKeyDown={(e) => e.stopPropagation()}
            onChange={(e) => e.target.value && set(e.target.value)}
          />
        </MenuItem>
      </Menu>
    </Stack>
  )
}
