import Alert from '@mui/material/Alert'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useQuestion } from '@/api/questions'
import { useTask } from '@/api/tasks'
import { AnswerCard, SourceList } from '@/components/AnswerCheck'
import { Section, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DoneWhen } from '@/components/DoneWhen'
import { LinkButton } from '@/components/links'
import { questionState } from '@/lib/answer'
import { nameList, nameOr } from '@/lib/names'

// QuestionPanel sits above a question document: its state, any proposed answer to check, why it
// is waiting, and the actions to edit it or change what it waits on.
export function QuestionPanel({ id }: { id: string }) {
  const q = useQuestion(id)
  return (
    <DataState q={q}>
      {({ question: x }) => {
        const st = questionState(x)
        return (
          <Section
            title="Question"
            action={
              <Stack direction="row" spacing={0.5}>
                {x.status === 'open' && (
                  <>
                    <LinkButton size="small" to="." search={(p) => ({ ...p, qedit: x.id })}>
                      Edit question
                    </LinkButton>
                    <LinkButton size="small" to="." search={(p) => ({ ...p, qdeps: x.id })}>
                      Waits on
                    </LinkButton>
                  </>
                )}
              </Stack>
            }
          >
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
              {x.hasProposal && x.status === 'open' && <AnswerCard q={x} />}
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

// TaskPanel shows how we know a task is done, and lets the owner set it.
export function TaskPanel({ id }: { id: string }) {
  const q = useTask(id)
  if (!q.data) return null
  const open = q.data.status !== 'done' && q.data.status !== 'dropped'
  return (
    <>
      <DoneWhen
        value={q.data.doneWhen}
        param="taskdw"
        id={id}
        prompt="Say how we know it is done"
      />
      {open && (
        <LinkButton
          size="small"
          variant="outlined"
          to="."
          search={(p) => ({ ...p, done: id })}
          sx={{ mb: 2 }}
        >
          Done
        </LinkButton>
      )}
    </>
  )
}
