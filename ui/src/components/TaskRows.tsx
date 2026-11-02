import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import { useQuestions } from '@/api/questions'
import { localDate } from '@/api/today'
import { Empty, StateChip, StatusChip } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'
import { RowMenu } from '@/components/RowMenu'
import { dueLabel, dueState } from '@/lib/due'
import { nameList, nameOr } from '@/lib/names'

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
  const questions = useQuestions()
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
