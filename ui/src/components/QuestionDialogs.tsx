import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import FormControl from '@mui/material/FormControl'
import FormControlLabel from '@mui/material/FormControlLabel'
import FormLabel from '@mui/material/FormLabel'
import MenuItem from '@mui/material/MenuItem'
import Radio from '@mui/material/Radio'
import RadioGroup from '@mui/material/RadioGroup'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import {
  useAnswer,
  useAskQuestion,
  useDropQuestion,
  useQuestion,
  useTaskToQuestion,
} from '@/api/questions'
import { useTaskDone } from '@/api/tasks'
import { useWork } from '@/api/work'
import { errorText } from '@/client/gqlClient'
import { joinNames, nameOr } from '@/lib/names'
import { dateWords } from '@/lib/when'
import { Frame, useClose } from '@/components/OverlayFrame'
import type { OverlaySearch } from '@/lib/overlays'
import type { Para_QuestionAskInputVia, Para_TaskToQuestionInputKind } from '@/gql/graphql'

type Props = { s: OverlaySearch; onDone: (m: string) => void }

export function QuestionDialogs({ s, onDone }: Props) {
  return (
    <>
      {s.answer && <AnswerDialog id={s.answer} onDone={onDone} />}
      {s.ask && <AskDialog id={s.ask} onDone={onDone} />}
      {s.drop && <DropDialog id={s.drop} onDone={onDone} />}
      {s.done && <DoneDialog id={s.done} onDone={onDone} />}
      {s.toq && <ToQuestionDialog id={s.toq} onDone={onDone} />}
    </>
  )
}

function AnswerDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const q = useQuestion(id)
  const answer = useAnswer()
  const work = useWork()
  const [choice, setChoice] = useState('')
  const [text, setText] = useState('')
  const [why, setWhy] = useState('')
  const [by, setBy] = useState('')
  const [source, setSource] = useState('')
  const question = q.data?.question
  const decision = question?.kind === 'decision'
  const options = question?.options ?? []
  const ok = (choice !== '' || text.trim() !== '') && (!decision || why.trim() !== '')

  return (
    <Frame
      title="Answer"
      onSubmit={() =>
        answer.mutate(
          {
            id,
            choice: choice || undefined,
            text: text.trim() || undefined,
            why: why.trim() || undefined,
            by: by.trim() || undefined,
            source: source.trim() || undefined,
          },
          {
            // One way to say done: a toast that names what was recorded and what it unblocked.
            onSuccess: (r) => {
              const titles = new Map((work.data?.tasks ?? []).map((t) => [t.id, t.title]))
              const names = r.unblocked.map((u) => titles.get(u) || nameOr('', u))
              const said = r.choice || text.trim()
              onDone(
                `Recorded ${said ? `"${said}"` : 'the answer'}${question?.title ? ` for "${question.title}"` : ''}. ${
                  names.length
                    ? `Unblocked ${joinNames(names)}.`
                    : 'Nothing else was waiting only on this.'
                }`,
              )
              close()
            },
          },
        )
      }
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={answer.isPending || !ok}>
            Record answer
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {q.isError && <Alert severity="error">{errorText(q.error)}</Alert>}
        {answer.isError && <Alert severity="error">{errorText(answer.error)}</Alert>}
        <Typography sx={{ overflowWrap: 'anywhere' }}>
          {question?.title ?? (q.isError ? '' : 'Loading')}
        </Typography>
        {options.length > 0 && (
          <FormControl>
            <FormLabel id="options-label">Options</FormLabel>
            <RadioGroup
              aria-labelledby="options-label"
              value={choice}
              onChange={(e) => setChoice(e.target.value)}
            >
              {options.map((o) => (
                <FormControlLabel
                  key={o}
                  value={o}
                  control={<Radio />}
                  label={o === question?.recommended ? `${o} (recommended)` : o}
                />
              ))}
            </RadioGroup>
          </FormControl>
        )}
        <TextField
          label={options.length > 0 ? 'Or answer in words' : 'Answer'}
          value={text}
          onChange={(e) => setText(e.target.value)}
          multiline
          minRows={2}
          fullWidth
          helperText="Never a secret: record where it is kept."
        />
        <TextField
          label={decision ? 'Why (required for a decision)' : 'Why (optional)'}
          value={why}
          onChange={(e) => setWhy(e.target.value)}
          required={decision}
          multiline
          minRows={2}
          fullWidth
        />
        <TextField
          label="Who told you (optional)"
          value={by}
          onChange={(e) => setBy(e.target.value)}
          fullWidth
        />
        <TextField
          label="Source (optional)"
          value={source}
          onChange={(e) => setSource(e.target.value)}
          fullWidth
        />
      </Stack>
    </Frame>
  )
}

const VIA: Para_QuestionAskInputVia[] = [
  'email',
  'slack',
  'teams',
  'meeting',
  'phone',
  'jira',
  'github',
  'confluence',
  'other',
]

function AskDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const ask = useAskQuestion()
  const [to, setTo] = useState('')
  const [route, setRoute] = useState('')
  const [via, setVia] = useState<Para_QuestionAskInputVia>('email')
  return (
    <Frame
      title="Ask someone"
      onSubmit={() =>
        ask.mutate(
          { id, to: to.trim(), route: route.trim(), via },
          {
            onSuccess: (r) => {
              onDone(r.warnings.length ? `Asked. ${r.warnings.join(' ')}` : `Asked in ${r.rfx}`)
              close()
            },
          },
        )
      }
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button
            type="submit"
            variant="contained"
            disabled={ask.isPending || to.trim() === '' || route.trim() === ''}
          >
            Ask
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {ask.isError && <Alert severity="error">{errorText(ask.error)}</Alert>}
        <TextField
          label="Who (name)"
          value={to}
          onChange={(e) => setTo(e.target.value)}
          required
          autoFocus
        />
        <TextField
          label="Route (the system or owner this goes through)"
          value={route}
          onChange={(e) => setRoute(e.target.value)}
          required
        />
        <TextField
          select
          label="By"
          value={via}
          onChange={(e) => setVia(e.target.value as Para_QuestionAskInputVia)}
        >
          {VIA.map((v) => (
            <MenuItem key={v} value={v}>
              {v}
            </MenuItem>
          ))}
        </TextField>
        <Typography variant="caption" color="text.secondary">
          Creates a request to that person and links it to the question. Para sends nothing.
        </Typography>
      </Stack>
    </Frame>
  )
}

function DropDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const drop = useDropQuestion()
  const q = useQuestion(id)
  const [reason, setReason] = useState('')
  return (
    <Frame
      title="Drop question"
      onSubmit={() =>
        drop.mutate(
          { id, reason: reason.trim() || undefined },
          {
            onSuccess: (r) => {
              onDone(
                `Dropped ${q.data?.question.title ? `"${q.data.question.title}"` : 'the question'}. ${r.unblocked.length ? `Unblocked ${r.unblocked.length}.` : 'Nothing else was waiting only on it.'}`,
              )
              close()
            },
          },
        )
      }
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" color="error" disabled={drop.isPending}>
            Drop
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {drop.isError && <Alert severity="error">{errorText(drop.error)}</Alert>}
        <Typography sx={{ overflowWrap: 'anywhere' }}>{q.data?.question.title ?? ''}</Typography>
        <TextField
          label="Reason (optional)"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          multiline
          minRows={2}
          autoFocus
          fullWidth
        />
      </Stack>
    </Frame>
  )
}

function DoneDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const done = useTaskDone()
  const work = useWork()
  const title = work.data?.tasks.find((t) => t.id === id)?.title
  const [note, setNote] = useState('')
  return (
    <Frame
      title="Finish task"
      onSubmit={() =>
        done.mutate(
          { id, note: note.trim() || undefined },
          {
            onSuccess: (r) => {
              onDone(
                `Finished ${title ? `"${title}"` : 'the task'}.${r.next ? ` Next recurrence ${dateWords(r.next)}.` : ''}`,
              )
              close()
            },
          },
        )
      }
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={done.isPending}>
            Done
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {done.isError && <Alert severity="error">{errorText(done.error)}</Alert>}
        <TextField
          label="Note (what was done or decided)"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          multiline
          minRows={2}
          autoFocus
          fullWidth
        />
      </Stack>
    </Frame>
  )
}

function ToQuestionDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const toq = useTaskToQuestion()
  const [kind, setKind] = useState<Para_TaskToQuestionInputKind>('question')
  const [options, setOptions] = useState('')
  const [recommended, setRecommended] = useState('')
  const list = options
    .split('\n')
    .map((o) => o.trim())
    .filter(Boolean)
  return (
    <Frame
      title="Turn the task into a question"
      onSubmit={() =>
        toq.mutate(
          {
            id,
            kind,
            options: list.length ? list : undefined,
            recommended: recommended.trim() || undefined,
          },
          {
            onSuccess: (r) => {
              onDone(`Now a question: ${r.question}`)
              close()
            },
          },
        )
      }
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={toq.isPending}>
            Convert
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {toq.isError && <Alert severity="error">{errorText(toq.error)}</Alert>}
        <TextField
          select
          label="Kind"
          value={kind}
          onChange={(e) => setKind(e.target.value as Para_TaskToQuestionInputKind)}
        >
          <MenuItem value="question">Question</MenuItem>
          <MenuItem value="decision">Decision (options and a reason)</MenuItem>
          <MenuItem value="input">Input (someone gives you a value)</MenuItem>
        </TextField>
        <TextField
          label="Options (one per line, optional)"
          value={options}
          onChange={(e) => setOptions(e.target.value)}
          multiline
          minRows={2}
          fullWidth
        />
        <TextField
          label="Recommended option (optional)"
          value={recommended}
          onChange={(e) => setRecommended(e.target.value)}
        />
        <Typography variant="caption" color="text.secondary">
          Tasks that waited on this task wait on the question instead.
        </Typography>
      </Stack>
    </Frame>
  )
}
