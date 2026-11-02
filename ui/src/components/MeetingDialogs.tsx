import { useState, type ReactNode } from 'react'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useAreas } from '@/api/areas'
import {
  useMeetingCancel,
  useMeetingHold,
  useMeetingSchedule,
  type HoldAction,
  type HoldInput,
  type useMeeting,
} from '@/api/meetings'
import { useQuestions } from '@/api/questions'
import { useWork } from '@/api/work'
import { errorText } from '@/client/gqlClient'
import { Pick } from '@/components/Pick'
import { questionIds, toLocalInput, toRFC3339, viewerZone } from '@/lib/meeting'
import { nameOr } from '@/lib/names'

type Meeting = NonNullable<ReturnType<typeof useMeeting>['data']>['meeting']

// Shell is a dialog that is open while its parent renders it, and closes through onClose.
function Shell({
  title,
  onClose,
  onSubmit,
  actions,
  children,
}: {
  title: string
  onClose: () => void
  onSubmit: () => void
  actions: ReactNode
  children: ReactNode
}) {
  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="sm" aria-labelledby="meeting-dialog-title">
      <form
        onSubmit={(e) => {
          e.preventDefault()
          onSubmit()
        }}
      >
        <DialogTitle id="meeting-dialog-title">{title}</DialogTitle>
        <DialogContent>
          <Stack spacing={1.5} sx={{ pt: 1 }}>
            {children}
          </Stack>
        </DialogContent>
        <DialogActions>{actions}</DialogActions>
      </form>
    </Dialog>
  )
}

// TimeFields is a start and an end in the viewer's zone. A start that moves past the end moves
// the end to an hour after it.
function TimeFields({
  start,
  end,
  setStart,
  setEnd,
}: {
  start: string
  end: string
  setStart: (v: string) => void
  setEnd: (v: string) => void
}) {
  const onStart = (v: string) => {
    setStart(v)
    const s = toRFC3339(v)
    if (s && (!end || Date.parse(toRFC3339(end)) <= Date.parse(s))) {
      setEnd(toLocalInput(new Date(Date.parse(s) + 3600_000).toISOString()))
    }
  }
  return (
    <>
      <TextField
        label="Start"
        type="datetime-local"
        value={start}
        onChange={(e) => onStart(e.target.value)}
        required
        slotProps={{ inputLabel: { shrink: true } }}
      />
      <TextField
        label="End"
        type="datetime-local"
        value={end}
        onChange={(e) => setEnd(e.target.value)}
        required
        slotProps={{ inputLabel: { shrink: true } }}
      />
      <Typography variant="caption" color="text.secondary">
        Times are in your zone, {viewerZone()}.
      </Typography>
    </>
  )
}

function timesOk(start: string, end: string): boolean {
  const s = Date.parse(toRFC3339(start))
  const e = Date.parse(toRFC3339(end))
  return !Number.isNaN(s) && !Number.isNaN(e) && e > s
}

export function ScheduleDialog({
  m,
  onClose,
  onDone,
}: {
  m: Meeting
  onClose: () => void
  onDone: (msg: string) => void
}) {
  const schedule = useMeetingSchedule()
  const [start, setStart] = useState(toLocalInput(m.start))
  const [end, setEnd] = useState(toLocalInput(m.end))
  const ok = timesOk(start, end)
  return (
    <Shell
      title={m.status === 'scheduled' ? 'Reschedule' : 'Schedule'}
      onClose={onClose}
      onSubmit={() =>
        schedule.mutate(
          { id: m.id, start: toRFC3339(start), end: toRFC3339(end) },
          {
            onSuccess: () => {
              onDone('Meeting scheduled')
              onClose()
            },
          },
        )
      }
      actions={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={schedule.isPending || !ok}>
            {m.status === 'scheduled' ? 'Reschedule' : 'Schedule'}
          </Button>
        </>
      }
    >
      {schedule.isError && <Alert severity="error">{errorText(schedule.error)}</Alert>}
      <TimeFields start={start} end={end} setStart={setStart} setEnd={setEnd} />
      {start !== '' && end !== '' && !ok && (
        <Alert severity="warning">The end must be after the start.</Alert>
      )}
    </Shell>
  )
}

export function CancelDialog({
  m,
  onClose,
  onDone,
}: {
  m: Meeting
  onClose: () => void
  onDone: (msg: string) => void
}) {
  const cancel = useMeetingCancel()
  const [reason, setReason] = useState('')
  return (
    <Shell
      title="Cancel meeting"
      onClose={onClose}
      onSubmit={() =>
        cancel.mutate(
          { id: m.id, reason: reason.trim() },
          {
            onSuccess: () => {
              onDone('Meeting cancelled')
              onClose()
            },
          },
        )
      }
      actions={
        <>
          <Button onClick={onClose}>Keep</Button>
          <Button type="submit" color="error" variant="contained" disabled={cancel.isPending}>
            Cancel meeting
          </Button>
        </>
      }
    >
      {cancel.isError && <Alert severity="error">{errorText(cancel.error)}</Alert>}
      <TextField
        label="Reason (optional)"
        value={reason}
        onChange={(e) => setReason(e.target.value)}
        multiline
        minRows={2}
        autoFocus
      />
    </Shell>
  )
}

type ActionRow = { title: string; assignee: string; due: string; doneWhen: string }
type ReplyRow = { from: string; outcome: 'answered' | 'partial' | 'no'; text: string; why: string }

const BLANK_ACTION: ActionRow = { title: '', assignee: '', due: '', doneWhen: '' }

// HoldDialog records what came of a meeting: notes, answers to its questions, replies to its
// asks, and action items. Anything left blank is skipped. An action becomes a task, so it needs
// a goal or an area; done when is optional because the owner is a person.
export function HoldDialog({
  m,
  body,
  onClose,
  onDone,
}: {
  m: Meeting
  body: string
  onClose: () => void
  onDone: (msg: string) => void
}) {
  const hold = useMeetingHold()
  const questions = useQuestions()
  const work = useWork()
  const areas = useAreas()
  const unbooked = m.status === 'requested' || !m.start
  const [notes, setNotes] = useState('')
  const [start, setStart] = useState(toLocalInput(m.start))
  const [end, setEnd] = useState(toLocalInput(m.end))
  const linked = new Set(questionIds(body))
  const open = (questions.data ?? []).filter((q) => linked.has(q.id) && q.status === 'open')
  const [answers, setAnswers] = useState<Record<string, string>>({})
  const [replies, setReplies] = useState<Record<string, ReplyRow>>({})
  const [actions, setActions] = useState<ActionRow[]>([])
  const [project, setProject] = useState('')
  const [area, setArea] = useState('')
  const reply = (id: string): ReplyRow =>
    replies[id] ?? { from: '', outcome: 'answered', text: '', why: '' }
  const setReply = (id: string, patch: Partial<ReplyRow>) =>
    setReplies((r) => ({ ...r, [id]: { ...reply(id), ...patch } }))
  const setAction = (i: number, patch: Partial<ActionRow>) =>
    setActions((a) => a.map((x, j) => (j === i ? { ...x, ...patch } : x)))

  const filled = actions.filter((a) => a.title.trim() !== '')
  const repliesOut = m.rfx.map((id) => ({ id, r: reply(id) })).filter((x) => x.r.text.trim() !== '')
  const replyOk = repliesOut.every(
    (x) => x.r.from.trim() !== '' && (x.r.outcome !== 'no' || x.r.why.trim() !== ''),
  )
  const actionsOk = filled.length === 0 || project !== '' || area !== ''
  const ok = (!unbooked || timesOk(start, end)) && replyOk && actionsOk

  const submit = () => {
    const input: HoldInput = {
      id: m.id,
      notes: notes.trim() || undefined,
      start: unbooked ? toRFC3339(start) : undefined,
      end: unbooked ? toRFC3339(end) : undefined,
      answers: open
        .filter((q) => (answers[q.id] ?? '').trim() !== '')
        .map((q) =>
          q.options.length > 0
            ? { id: q.id, choice: answers[q.id] }
            : { id: q.id, text: (answers[q.id] ?? '').trim() },
        ),
      replies: repliesOut.map((x) => ({
        id: x.id,
        from: x.r.from.trim(),
        outcome: x.r.outcome,
        text: x.r.text.trim(),
        why: x.r.why.trim() || undefined,
      })),
      actions: filled.map((a): HoldAction => ({
        title: a.title.trim(),
        assignee: a.assignee.trim() || undefined,
        due: a.due || undefined,
        doneWhen: a.doneWhen.trim() || undefined,
      })),
      project: project || undefined,
      area: area || undefined,
    }
    hold.mutate(input, {
      onSuccess: (r) => {
        onDone(
          `Meeting held. ${r.tasks.length} action item${r.tasks.length === 1 ? '' : 's'}, ${r.answered.length} answer${r.answered.length === 1 ? '' : 's'}.`,
        )
        onClose()
      },
    })
  }

  return (
    <Shell
      title="Hold meeting"
      onClose={onClose}
      onSubmit={submit}
      actions={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={hold.isPending || !ok}>
            Record as held
          </Button>
        </>
      }
    >
      {hold.isError && <Alert severity="error">{errorText(hold.error)}</Alert>}
      {unbooked && (
        <>
          <Typography variant="body2">
            This meeting was never scheduled. Say when it was held.
          </Typography>
          <TimeFields start={start} end={end} setStart={setStart} setEnd={setEnd} />
        </>
      )}
      <TextField
        label="Notes"
        value={notes}
        onChange={(e) => setNotes(e.target.value)}
        multiline
        minRows={3}
        autoFocus
      />
      {open.length > 0 && (
        <>
          <Typography variant="subtitle2" component="h3">
            Answers
          </Typography>
          {open.map((q) =>
            q.options.length > 0 ? (
              <TextField
                key={q.id}
                select
                label={q.title}
                value={answers[q.id] ?? ''}
                onChange={(e) => setAnswers((a) => ({ ...a, [q.id]: e.target.value }))}
              >
                <MenuItem value="">Not answered</MenuItem>
                {q.options.map((o) => (
                  <MenuItem key={o} value={o}>
                    {o}
                  </MenuItem>
                ))}
              </TextField>
            ) : (
              <TextField
                key={q.id}
                label={q.title}
                value={answers[q.id] ?? ''}
                onChange={(e) => setAnswers((a) => ({ ...a, [q.id]: e.target.value }))}
                multiline
                helperText="Leave blank if it was not answered."
              />
            ),
          )}
        </>
      )}
      {m.rfx.length > 0 && (
        <>
          <Typography variant="subtitle2" component="h3">
            Replies to asks
          </Typography>
          {m.rfx.map((id, i) => {
            const r = reply(id)
            return (
              <Stack key={id} spacing={1}>
                <Typography variant="body2">{nameOr(m.rfxNames?.[i], id)}</Typography>
                <TextField
                  label="What they said"
                  value={r.text}
                  onChange={(e) => setReply(id, { text: e.target.value })}
                  multiline
                  helperText="Leave blank for no reply."
                />
                {r.text.trim() !== '' && (
                  <>
                    <TextField
                      label="Who replied (name)"
                      value={r.from}
                      onChange={(e) => setReply(id, { from: e.target.value })}
                      required
                    />
                    <TextField
                      select
                      label="Outcome"
                      value={r.outcome}
                      onChange={(e) =>
                        setReply(id, { outcome: e.target.value as ReplyRow['outcome'] })
                      }
                    >
                      <MenuItem value="answered">Answered</MenuItem>
                      <MenuItem value="partial">Partly answered</MenuItem>
                      <MenuItem value="no">No (creates a constraint)</MenuItem>
                    </TextField>
                    {r.outcome === 'no' && (
                      <TextField
                        label="Why"
                        value={r.why}
                        onChange={(e) => setReply(id, { why: e.target.value })}
                        required
                      />
                    )}
                  </>
                )}
              </Stack>
            )
          })}
        </>
      )}
      <Typography variant="subtitle2" component="h3">
        Action items
      </Typography>
      {actions.map((a, i) => (
        <Stack key={i} spacing={1} sx={{ borderLeft: 2, borderColor: 'divider', pl: 1.5 }}>
          <TextField
            label="Action"
            value={a.title}
            onChange={(e) => setAction(i, { title: e.target.value })}
            required
          />
          <TextField
            label="For (name, blank for you)"
            value={a.assignee}
            onChange={(e) => setAction(i, { assignee: e.target.value })}
          />
          <TextField
            label="Due"
            type="date"
            value={a.due}
            onChange={(e) => setAction(i, { due: e.target.value })}
            slotProps={{ inputLabel: { shrink: true } }}
          />
          <TextField
            label="Done when (optional)"
            value={a.doneWhen}
            onChange={(e) => setAction(i, { doneWhen: e.target.value })}
          />
          <Button
            size="small"
            color="error"
            sx={{ alignSelf: 'flex-start' }}
            onClick={() => setActions((x) => x.filter((_, j) => j !== i))}
          >
            Remove action
          </Button>
        </Stack>
      ))}
      <Button
        size="small"
        sx={{ alignSelf: 'flex-start' }}
        onClick={() => setActions((x) => [...x, { ...BLANK_ACTION }])}
      >
        Add action item
      </Button>
      {filled.length > 0 && (
        <>
          <Pick
            label="Goal for the actions"
            value={project}
            onChange={setProject}
            options={(work.data?.projects ?? []).filter((p) => p.status !== 'done')}
            none="None"
          />
          <Pick
            label="Or area for the actions"
            value={area}
            onChange={setArea}
            options={areas.data ?? []}
            none="None"
          />
          {!actionsOk && <Alert severity="warning">An action item needs a goal or an area.</Alert>}
        </>
      )}
    </Shell>
  )
}
