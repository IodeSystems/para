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
  useCut,
  useDefer,
  useDropQuestion,
  useEditQuestion,
  useInvestigate,
  useQuestion,
  useQuestionDepends,
  useQuestions,
  useRejectAnswer,
  useTaskToQuestion,
} from '@/api/questions'
import { useGoalEdit } from '@/api/areas'
import { useTaskDone, useTaskEdit } from '@/api/tasks'
import { Pick } from '@/components/Pick'
import { doneConfirm } from '@/lib/answer'
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
      {s.later && <LaterDialog ids={s.later.split(',')} onDone={onDone} />}
      {s.find && <FindDialog id={s.find} onDone={onDone} />}
      {s.cut && <CutDialog id={s.cut} onDone={onDone} />}
      {s.done && <DoneDialog id={s.done} onDone={onDone} />}
      {s.toq && <ToQuestionDialog id={s.toq} onDone={onDone} />}
      {s.reject && <RejectDialog id={s.reject} onDone={onDone} />}
      {s.qedit && <QuestionEditDialog id={s.qedit} onDone={onDone} />}
      {s.qdeps && <DependsDialog id={s.qdeps} onDone={onDone} />}
      {s.taskdw && <DoneWhenDialog kind="task" id={s.taskdw} onDone={onDone} />}
      {s.goaldw && <DoneWhenDialog kind="goal" id={s.goaldw} onDone={onDone} />}
    </>
  )
}

function AnswerDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const q = useQuestion(id)
  const answer = useAnswer()
  const work = useWork()
  // null means the person has not picked: the recommended option stands until they do.
  const [picked, setPicked] = useState<string | null>(null)
  const [text, setText] = useState('')
  const [why, setWhy] = useState('')
  const [by, setBy] = useState('')
  const [source, setSource] = useState('')
  // A person may give several sources: a URL, a document id, or words such as a meeting, each
  // with a quote. All optional.
  const [sources, setSources] = useState<{ ref: string; quote: string }[]>([{ ref: '', quote: '' }])
  const [more, setMore] = useState(false)
  const question = q.data?.question
  const decision = question?.kind === 'decision'
  const options = question?.options ?? []
  const choice = text.trim() !== '' ? '' : (picked ?? question?.recommended ?? '')
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
            sources: sources
              .filter((x) => x.ref.trim() !== '')
              .map((x) => ({ ref: x.ref.trim(), quote: x.quote.trim() || undefined })),
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
              onChange={(e) => {
                setPicked(e.target.value)
                setText('')
              }}
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
          label={decision ? 'Reason (required for a decision)' : 'Reason (optional)'}
          value={why}
          onChange={(e) => setWhy(e.target.value)}
          required={decision}
          fullWidth
        />
        <Typography variant="subtitle2">Sources (optional)</Typography>
        {sources.map((x, i) => (
          <Stack key={i} spacing={1}>
            <TextField
              label={
                i === 0
                  ? 'Source: a URL, a document id, or words such as a meeting'
                  : 'Another source'
              }
              value={x.ref}
              onChange={(e) =>
                setSources(sources.map((y, j) => (j === i ? { ...y, ref: e.target.value } : y)))
              }
              fullWidth
            />
            {x.ref.trim() !== '' && (
              <TextField
                label="Quote from it (optional)"
                value={x.quote}
                onChange={(e) =>
                  setSources(sources.map((y, j) => (j === i ? { ...y, quote: e.target.value } : y)))
                }
                fullWidth
              />
            )}
          </Stack>
        ))}
        {sources[sources.length - 1]?.ref.trim() !== '' && (
          <Button
            size="small"
            color="inherit"
            onClick={() => setSources([...sources, { ref: '', quote: '' }])}
            sx={{ alignSelf: 'flex-start' }}
          >
            Add another source
          </Button>
        )}
        <Button
          size="small"
          color="inherit"
          onClick={() => setMore(!more)}
          aria-expanded={more}
          sx={{ alignSelf: 'flex-start' }}
        >
          More
        </Button>
        {more && (
          <>
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
          </>
        )}
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
          Creates an ask to that person and links it to the question. Para sends nothing.
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

function LaterDialog({ ids, onDone }: { ids: string[]; onDone: (m: string) => void }) {
  const close = useClose()
  const later = useDefer()
  const [until, setUntil] = useState('')
  const [reason, setReason] = useState('')
  return (
    <Frame
      title={`Later: ${ids.length} item${ids.length === 1 ? '' : 's'}`}
      onSubmit={() =>
        later.mutate(
          { ids, until: until || undefined, reason: reason.trim() || undefined },
          {
            onSuccess: (r) => {
              onDone(
                `Put off ${r.changed.length}${until ? ` until ${dateWords(until)}` : ' until someday'}.${r.skipped.length ? ` Left as they were: ${r.skipped.length}.` : ''}`,
              )
              close()
            },
          },
        )
      }
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={later.isPending}>
            Put off
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {later.isError && <Alert severity="error">{errorText(later.error)}</Alert>}
        <TextField
          label="Back on (leave empty for someday)"
          type="date"
          value={until}
          onChange={(e) => setUntil(e.target.value)}
          slotProps={{ inputLabel: { shrink: true } }}
          fullWidth
        />
        <TextField
          label="Why it can wait (optional)"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          fullWidth
        />
        <Typography variant="caption" color="text.secondary">
          It leaves Today and the task lists. It still blocks what depends on it. To go ahead
          without it, cut the dependency instead.
        </Typography>
      </Stack>
    </Frame>
  )
}

function FindDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const find = useInvestigate()
  const q = useQuestion(id)
  const [how, setHow] = useState('')
  return (
    <Frame
      title="Find evidence"
      onSubmit={() =>
        find.mutate(
          { id, how: how.trim() },
          {
            onSuccess: () => {
              onDone('Queued agent work to find evidence. The question stays open.')
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
            disabled={find.isPending || how.trim() === '' || q.data?.question.agentLooking}
          >
            Send to an agent
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {find.isError && <Alert severity="error">{errorText(find.error)}</Alert>}
        {q.data?.question.agentLooking && (
          <Alert severity="info">An agent is looking already.</Alert>
        )}
        <Typography sx={{ overflowWrap: 'anywhere' }}>{q.data?.question.title ?? ''}</Typography>
        <TextField
          label="Where to look (for example: check the wiki, search #platform)"
          value={how}
          onChange={(e) => setHow(e.target.value)}
          multiline
          minRows={2}
          autoFocus
          fullWidth
        />
        <Typography variant="caption" color="text.secondary">
          The agent proposes an answer with its sources. You check it as you check any answer. Needs
          a runner set up on this machine; otherwise the work waits under Agent work.
        </Typography>
      </Stack>
    </Frame>
  )
}

function CutDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const cut = useCut()
  const q = useQuestion(id)
  const work = useWork()
  const [pick, setPick] = useState('')
  const [reason, setReason] = useState('')
  // A question: each thing waiting on it. A task: what it waits on.
  const titles = new Map((work.data?.tasks ?? []).map((t) => [t.id, t.title]))
  const task = (work.data?.tasks ?? []).find((t) => t.id === id)
  const pairs: { waiter: string; dep: string; label: string }[] = q.data
    ? [
        ...q.data.question.blocks.map((b) => ({
          waiter: b,
          dep: id,
          label: `${titles.get(b) || nameOr('', b)} stops waiting on this question`,
        })),
        ...q.data.question.waitingOn.map((w) => ({
          waiter: id,
          dep: w,
          label: `This question stops waiting on ${titles.get(w) || nameOr('', w)}`,
        })),
      ]
    : (task?.dependsOn ?? []).map((d) => ({
        waiter: id,
        dep: d,
        label: `${task?.title ?? 'This task'} stops waiting on ${titles.get(d) || nameOr('', d)}`,
      }))
  const chosen = pairs[Number(pick || '0')]
  return (
    <Frame
      title="Cut a dependency"
      onSubmit={() =>
        chosen &&
        cut.mutate(
          { id: chosen.waiter, dep: chosen.dep, reason: reason.trim() },
          {
            onSuccess: () => {
              onDone('Cut. The reason is on it as an assumption.')
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
            disabled={cut.isPending || !chosen || reason.trim() === ''}
          >
            Cut
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {cut.isError && <Alert severity="error">{errorText(cut.error)}</Alert>}
        {pairs.length === 0 && <Alert severity="info">Nothing here waits or is waited on.</Alert>}
        <TextField
          select
          label="Cut which"
          value={pick || '0'}
          onChange={(e) => setPick(e.target.value)}
        >
          {pairs.map((p, i) => (
            <MenuItem key={`${p.waiter}-${p.dep}`} value={String(i)}>
              {p.label}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          label="Why it can go ahead without it (required)"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          multiline
          minRows={2}
          fullWidth
        />
        <Typography variant="caption" color="text.secondary">
          The reason is recorded on the task as an assumption, and shows in its history.
        </Typography>
      </Stack>
    </Frame>
  )
}

function DoneDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const done = useTaskDone()
  const work = useWork()
  const task = work.data?.tasks.find((t) => t.id === id)
  const title = task?.title
  const confirm = doneConfirm(task?.doneWhen)
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
            {confirm ? 'Yes, done' : 'Done'}
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {done.isError && <Alert severity="error">{errorText(done.error)}</Alert>}
        {confirm && <Typography sx={{ overflowWrap: 'anywhere' }}>{confirm}</Typography>}
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

function RejectDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const reject = useRejectAnswer()
  const q = useQuestion(id)
  const [reason, setReason] = useState('')
  return (
    <Frame
      title="Reject the proposed answer"
      onSubmit={() =>
        reject.mutate(
          { id, reason: reason.trim() },
          {
            onSuccess: () => {
              onDone('Rejected. The agent can read your reason.')
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
            color="error"
            disabled={reject.isPending || reason.trim() === ''}
          >
            Reject
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {reject.isError && <Alert severity="error">{errorText(reject.error)}</Alert>}
        <Typography sx={{ overflowWrap: 'anywhere' }}>{q.data?.question.title ?? ''}</Typography>
        <TextField
          label="Reason (required)"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          multiline
          minRows={2}
          autoFocus
          required
          fullWidth
          helperText="The question stays open. The agent reads this."
        />
      </Stack>
    </Frame>
  )
}

function QuestionEditDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const q = useQuestion(id)
  const edit = useEditQuestion()
  const question = q.data?.question
  // The form starts from the loaded question, once.
  const [form, setForm] = useState<{ title: string; options: string; recommended: string } | null>(
    null,
  )
  const f = form ?? {
    title: question?.title ?? '',
    options: (question?.options ?? []).join('\n'),
    recommended: question?.recommended ?? '',
  }
  const set = (patch: Partial<typeof f>) => setForm({ ...f, ...patch })
  const list = f.options
    .split('\n')
    .map((o) => o.trim())
    .filter(Boolean)
  return (
    <Frame
      title="Edit question"
      onSubmit={() =>
        edit.mutate(
          {
            id,
            title: f.title.trim() || undefined,
            options: list.length ? list : undefined,
            clearOptions: list.length === 0 ? true : undefined,
            recommended: f.recommended.trim() || undefined,
            clearRecommended: f.recommended.trim() === '' ? true : undefined,
          },
          {
            onSuccess: () => {
              onDone('Question changed')
              close()
            },
          },
        )
      }
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={edit.isPending || !question}>
            Save
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {q.isError && <Alert severity="error">{errorText(q.error)}</Alert>}
        {edit.isError && <Alert severity="error">{errorText(edit.error)}</Alert>}
        <TextField
          label="Question"
          value={f.title}
          onChange={(e) => set({ title: e.target.value })}
          multiline
          fullWidth
          autoFocus
        />
        <TextField
          label="Options (one per line)"
          value={f.options}
          onChange={(e) => set({ options: e.target.value })}
          multiline
          minRows={2}
          fullWidth
        />
        <TextField
          label="Recommended option"
          value={f.recommended}
          onChange={(e) => set({ recommended: e.target.value })}
        />
      </Stack>
    </Frame>
  )
}

function DependsDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const q = useQuestion(id)
  const open = useQuestions()
  const depends = useQuestionDepends()
  const [pick, setPick] = useState('')
  const have = q.data?.question.dependsOn ?? []
  const options = (open.data ?? []).filter((x) => x.id !== id && !have.includes(x.id))
  const run = (dependsOn: string, remove?: boolean) =>
    depends.mutate(
      { id, dependsOn: [dependsOn], remove },
      { onSuccess: () => onDone(remove ? 'Dependency removed' : 'Dependency added') },
    )
  return (
    <Frame
      title="What this question waits on"
      onSubmit={() => pick && run(pick)}
      actions={
        <>
          <Button onClick={close}>Close</Button>
          <Button type="submit" variant="contained" disabled={depends.isPending || pick === ''}>
            Add
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {depends.isError && <Alert severity="error">{errorText(depends.error)}</Alert>}
        <Typography sx={{ overflowWrap: 'anywhere' }}>{q.data?.question.title ?? ''}</Typography>
        {have.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            It waits on nothing.
          </Typography>
        ) : (
          have.map((d) => (
            <Stack key={d} direction="row" spacing={1} sx={{ alignItems: 'center' }}>
              <Typography variant="body2" sx={{ flexGrow: 1, overflowWrap: 'anywhere' }}>
                {nameOr('', d)}
              </Typography>
              <Button
                size="small"
                disabled={depends.isPending}
                aria-label={`Remove dependency ${nameOr('', d)}`}
                onClick={() => run(d, true)}
              >
                Remove
              </Button>
            </Stack>
          ))
        )}
        <Pick
          label="Add: wait on this question"
          value={pick}
          onChange={setPick}
          options={options}
        />
      </Stack>
    </Frame>
  )
}

// DoneWhenDialog sets how we know a task or a goal is done.
function DoneWhenDialog({
  kind,
  id,
  onDone,
}: {
  kind: 'task' | 'goal'
  id: string
  onDone: (m: string) => void
}) {
  const close = useClose()
  const task = useTaskEdit()
  const goal = useGoalEdit()
  const work = useWork()
  const current = kind === 'task' ? work.data?.tasks.find((t) => t.id === id)?.doneWhen : undefined
  const [text, setText] = useState<string | null>(null)
  const value = text ?? current ?? ''
  const m = kind === 'task' ? task : goal
  return (
    <Frame
      title={kind === 'task' ? 'Done when (task)' : 'Done when (goal)'}
      onSubmit={() =>
        m.mutate(
          { id, doneWhen: value.trim() },
          {
            onSuccess: () => {
              onDone('Saved how we know it is done')
              close()
            },
          },
        )
      }
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={m.isPending || value.trim() === ''}>
            Save
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {m.isError && <Alert severity="error">{errorText(m.error)}</Alert>}
        <TextField
          label="How we know it is done"
          value={value}
          onChange={(e) => setText(e.target.value)}
          multiline
          minRows={2}
          autoFocus
          required
          fullWidth
        />
      </Stack>
    </Frame>
  )
}
