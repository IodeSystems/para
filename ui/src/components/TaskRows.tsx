import { useState } from 'react'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import { useQuestions } from '@/api/questions'
import { localDate } from '@/api/today'
import { Empty, StateChip, StatusChip } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'
import { ItemDates } from '@/components/ItemDates'
import { ItemActions } from '@/components/AgendaActions'
import { BulkList, SelectBox } from '@/components/BulkSelect'
import { PartOfLine } from '@/components/PartOf'
import type { PartRef } from '@/lib/partof'
import { isClosed, splitDone } from '@/lib/agenda'
import { AgentLooking, AgentWorking } from '@/components/FindContext'
import { PinButton } from '@/components/PinButton'
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
  outcome?: string | null
  deferred?: boolean
  deferUntil?: string | null
  laterText?: string | null
  waitsOn?: string[]
  waitsOnNames?: string[] | null
  created?: string | null
  updated?: string | null
  partOf?: PartRef[] | null
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

// Each row says what the task is part of (its parent, obligation, goal, tags), as links; omit leaves
// out the page the list is on, such as a goal page's own goal. showProject is the older plain
// project name, for a row with no Part of. Done and
// dropped tasks are hidden until Show done; each row has Done, Later, or Reopen, and Select picks
// rows for one decision on all of them (BulkList).
export function TaskRows({
  tasks,
  showProject,
  omit,
}: {
  tasks: TaskRow[]
  showProject?: boolean
  omit?: string
}) {
  const questions = useQuestions({ deferred: true })
  const [showDone, setShowDone] = useState(false)
  // task id -> titles of the open questions it waits on
  const waits = new Map<string, string[]>()
  for (const q of questions.data ?? [])
    for (const id of q.blocks) waits.set(id, [...(waits.get(id) ?? []), q.title || q.id])
  const { open: openRows, done: doneRows } = splitDone(tasks, (t) => isClosed(t.status))
  const shown = showDone ? [...openRows, ...doneRows] : openRows
  if (tasks.length === 0) return <Empty>No tasks.</Empty>
  return (
    <>
      {shown.length === 0 && <Empty>No open task.</Empty>}
      <BulkList ids={shown.map((t) => t.id)}>
        <List dense disablePadding>
          {shown.map((t) => {
            const open = !isClosed(t.status)
            return (
              <ListItem key={t.id} disableGutters divider sx={{ display: 'block' }}>
                <Stack direction="row" spacing={1} sx={{ alignItems: 'flex-start' }}>
                  <SelectBox id={t.id} title={t.title} />
                  <ListItemText
                    primary={<DocLink id={t.id}>{t.title || t.id}</DocLink>}
                    secondary={
                      <>
                        <PartOfLine parts={t.partOf} omit={omit} />
                        {[
                          showProject && t.project && !t.partOf?.length
                            ? nameOr(t.projectName, t.project)
                            : undefined,
                          waitingOn(t, waits),
                          open && t.doneWhen ? `Done when: ${t.doneWhen}` : undefined,
                        ]
                          .filter(Boolean)
                          .join(' - ') || null}
                      </>
                    }
                    slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
                  />
                  <PinButton id={t.id} />
                  <ItemActions
                    x={{ id: t.id, title: t.title, kind: 'task' }}
                    closed={!open}
                    deferred={!!t.deferred}
                    open={false}
                  />
                </Stack>
                <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', mt: 0.5 }}>
                  {t.status !== 'todo' && <StateChip state={t.status} />}
                  {t.due && open && <DueChip due={t.due} />}
                  {t.deferred && (
                    <StatusChip
                      tone="quiet"
                      label={laterLabel(
                        t.deferUntil || (t.waitsOn?.length ? null : 'someday'),
                        t.waitsOn,
                        t.waitsOnNames,
                        t.laterText,
                      )}
                    />
                  )}
                  {t.outcome && t.outcome !== 'done' && (
                    <StatusChip tone="quiet" label={t.outcome} />
                  )}
                  <AgentLooking id={t.id} />
                  <AgentWorking id={t.id} />
                  {t.recur && <StatusChip tone="quiet" label={`repeats ${t.recur}`} />}
                  {t.assignee && (
                    <StatusChip tone="quiet" label={`with ${nameOr(t.assigneeName, t.assignee)}`} />
                  )}
                  <ItemDates created={t.created} updated={t.updated} />
                </Stack>
              </ListItem>
            )
          })}
        </List>
      </BulkList>
      {doneRows.length > 0 && (
        <Button
          size="small"
          color="inherit"
          aria-pressed={showDone}
          onClick={() => setShowDone((v) => !v)}
        >
          {showDone ? 'Hide done' : `Show done (${doneRows.length})`}
        </Button>
      )}
    </>
  )
}
