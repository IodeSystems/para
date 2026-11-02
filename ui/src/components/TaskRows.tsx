import Chip from '@mui/material/Chip'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import { useQuestions } from '@/api/questions'
import { localDate } from '@/api/today'
import { Empty, StateChip } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'
import { LinkButton } from '@/components/links'
import { dueLabel, dueState } from '@/lib/due'

export type TaskRow = {
  id: string
  title: string
  status: string
  due?: string | null
  recur?: string | null
  assignee?: string | null
  dependsOn: string[]
  blockedBy: string[]
}

// DueChip shows a due date. An overdue task is red, one due today or soon is amber.
export function DueChip({ due, today = localDate() }: { due: string; today?: string }) {
  const s = dueState(due, today)
  return (
    <Chip
      size="small"
      label={dueLabel(due, today)}
      color={s === 'overdue' ? 'error' : s === 'today' || s === 'soon' ? 'warning' : 'default'}
      variant={s === 'overdue' ? 'filled' : 'outlined'}
    />
  )
}

// waitingOn says why a task cannot start: an open question first, then a block, then the tasks it
// follows. An answered question is not a reason, so its id is not listed.
function waitingOn(t: TaskRow, open: Map<string, string[]>): string | undefined {
  const qs = open.get(t.id)
  if (qs?.length) return `waiting on question: ${qs.join('; ')}`
  if (t.blockedBy.length) return `blocked by ${t.blockedBy.join(', ')}`
  const after = t.dependsOn.filter((d) => !d.startsWith('question-'))
  return after.length ? `after ${after.join(', ')}` : undefined
}

export function TaskRows({ tasks }: { tasks: TaskRow[] }) {
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
          <ListItem key={t.id} disableGutters divider sx={{ display: 'block' }}>
            <Stack direction="row" spacing={1} sx={{ alignItems: 'flex-start' }}>
              <ListItemText
                primary={<DocLink id={t.id}>{t.title || t.id}</DocLink>}
                secondary={waitingOn(t, waits)}
                slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
              />
              <StateChip state={t.status} />
            </Stack>
            <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', mt: 0.5 }}>
              {t.due && open && <DueChip due={t.due} />}
              {t.recur && <Chip size="small" variant="outlined" label={`repeats ${t.recur}`} />}
              {t.assignee && <Chip size="small" variant="outlined" label={`with ${t.assignee}`} />}
              {open && (
                <>
                  <LinkButton size="small" to="." search={(p) => ({ ...p, done: t.id })}>
                    Done
                  </LinkButton>
                  <LinkButton
                    size="small"
                    color="inherit"
                    to="."
                    search={(p) => ({ ...p, toq: t.id })}
                  >
                    To question
                  </LinkButton>
                </>
              )}
            </Stack>
          </ListItem>
        )
      })}
    </List>
  )
}
