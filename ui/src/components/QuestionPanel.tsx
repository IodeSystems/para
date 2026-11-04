import Alert from '@mui/material/Alert'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useQuestion } from '@/api/questions'
import { useTask, useTaskParts } from '@/api/tasks'
import { DocLink } from '@/components/DocLink'
import { ProposedAnswer, SourceList } from '@/components/AnswerCheck'
import { Section, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DoneWhen } from '@/components/DoneWhen'
import { questionState } from '@/lib/answer'
import { nameList, nameOr } from '@/lib/names'

// QuestionPanel sits above a question document: its state, any older proposed answer, and why it
// is waiting.
export function QuestionPanel({ id }: { id: string }) {
  const q = useQuestion(id)
  return (
    <DataState q={q}>
      {({ question: x }) => {
        const st = questionState(x)
        return (
          <Section title="Question">
            <Stack spacing={1}>
              <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
                <StateChip {...st} />
                {x.waitingOn.length > 0 && x.status === 'open' && (
                  <Typography variant="body2" color="text.secondary">
                    Waiting on {nameList(x.waitingOn, null).join(', ')}
                  </Typography>
                )}
              </Stack>
              {x.rejectedReason && <Alert severity="warning">Rejected: {x.rejectedReason}</Alert>}
              {x.hasProposal && x.status === 'open' && <ProposedAnswer q={x} />}
              {x.status === 'answered' && (x.choice || x.answeredBy) && (
                <Typography variant="body2">
                  Answered{x.choice ? `: ${x.choice}` : ''}
                  {x.answeredBy ? ` (${nameOr('', x.answeredBy)})` : ''}
                </Typography>
              )}
              {x.status !== 'open' && <SourceList sources={x.proposedSources} />}
            </Stack>
          </Section>
        )
      }}
    </DataState>
  )
}

// TaskPanel shows a task's parts and how we know it is done.
export function TaskPanel({ id }: { id: string }) {
  const q = useTask(id)
  const parts = useTaskParts(id).data
  if (!q.data) return null
  const open = q.data.status !== 'done' && q.data.status !== 'dropped'
  const p = parts?.parts
  const groups: { title: string; rows: { id: string; title: string; note: string }[] }[] = p
    ? [
        {
          title: 'Action items',
          rows: p.actionItems.map((r) => ({
            id: r.id,
            title: r.title,
            note: [r.status, r.due].filter(Boolean).join(', '),
          })),
        },
        {
          title: 'Open questions',
          rows: (parts?.questions ?? []).map((r) => ({ id: r.id, title: r.title, note: r.kind })),
        },
        {
          title: 'Answered',
          rows: p.answered.map((r) => ({ id: r.id, title: r.title, note: r.choice ?? '' })),
        },
        {
          title: 'Commitments',
          rows: p.commitments.map((r) => ({
            id: r.id,
            title: r.title,
            note: [r.owner || 'you', r.due ? `due ${r.due}` : '', r.status]
              .filter(Boolean)
              .join(', '),
          })),
        },
        {
          title: 'Policy',
          rows: p.policy.map((r) => ({ id: r.id, title: r.title, note: r.kind })),
        },
        {
          title: 'Follow-ups',
          rows: p.followUps.map((r) => ({
            id: r.id,
            title: r.title,
            note: [r.status, r.due].filter(Boolean).join(', '),
          })),
        },
      ]
    : []
  return (
    <>
      {q.data.parent && (
        <Typography variant="body2" sx={{ mb: 1 }}>
          Action item of <DocLink id={q.data.parent}>{q.data.parentName || q.data.parent}</DocLink>
        </Typography>
      )}
      {groups
        .filter((g) => g.rows.length > 0)
        .map((g) => (
          <Section key={g.title} title={g.title} count={g.rows.length}>
            <Stack spacing={0.5}>
              {g.rows.map((r) => (
                <Typography key={r.id} variant="body2" sx={{ overflowWrap: 'anywhere' }}>
                  <DocLink id={r.id}>{r.title || r.id}</DocLink>
                  {r.note ? ` (${r.note})` : ''}
                </Typography>
              ))}
            </Stack>
          </Section>
        ))}
      <DoneWhen value={q.data.doneWhen} prompt="No done-when yet." />
      {!open && q.data.outcome && q.data.outcome !== 'done' && (
        <Typography variant="body2" sx={{ mb: 2 }}>
          Closed as {q.data.outcome}.
        </Typography>
      )}
    </>
  )
}
