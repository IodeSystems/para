import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useTask, useTaskAssign, useTaskBlock, useTaskDelegate, useTaskResolve } from '@/api/tasks'
import { useWorking } from '@/api/requests'
import { doneConfirm } from '@/lib/answer'
import { errorText } from '@/client/gqlClient'
import { dateWords } from '@/lib/when'
import { Frame, useClose } from '@/components/OverlayFrame'
import type { OverlaySearch } from '@/lib/overlays'
import type { Para_TaskResolveInputOutcome } from '@/gql/graphql'

type Props = { s: OverlaySearch; onDone: (m: string) => void }

// TaskDialogs are the dialogs of the task actions: Resolve, Block, Assign, Give to agent.
export function TaskDialogs({ s, onDone }: Props) {
  return (
    <>
      {(s.resolve || s.done) && <ResolveDialog id={(s.resolve || s.done) ?? ''} onDone={onDone} />}
      {s.block && <BlockDialog id={s.block} onDone={onDone} />}
      {s.assign && <AssignDialog id={s.assign} onDone={onDone} />}
      {s.delegate && <DelegateDialog id={s.delegate} onDone={onDone} />}
    </>
  )
}

const OUTCOMES: { value: Para_TaskResolveInputOutcome; label: string; hint: string }[] = [
  { value: 'done', label: 'Done', hint: 'It was finished.' },
  {
    value: 'partial',
    label: 'Partly done',
    hint: 'Some of it is done. Say what is left as a follow-up.',
  },
  {
    value: 'dropped',
    label: 'Dropped',
    hint: 'It is not needed. Anything waiting on it is released.',
  },
  {
    value: 'duplicate',
    label: 'Duplicate',
    hint: 'Another item covers it. Waiting items move to that one.',
  },
  {
    value: 'superseded',
    label: 'Superseded',
    hint: 'Another item replaces it. Waiting items move to that one.',
  },
]

function ResolveDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const resolve = useTaskResolve()
  const t = useTask(id)
  const task = t.data
  const [outcome, setOutcome] = useState<Para_TaskResolveInputOutcome>('done')
  const [note, setNote] = useState('')
  const [link, setLink] = useState('')
  const [followUps, setFollowUps] = useState('')
  const [doneWhen, setDoneWhen] = useState<string | null>(null)
  const dw = doneWhen ?? task?.doneWhen ?? ''
  const changed = !!task?.doneWhen && dw.trim() !== '' && dw.trim() !== task.doneWhen
  const titles = followUps
    .split('\n')
    .map((x) => x.trim())
    .filter(Boolean)
  const needsLink = outcome === 'duplicate' || outcome === 'superseded'
  const ok =
    (outcome !== 'dropped' || note.trim() !== '') &&
    (outcome !== 'partial' || note.trim() !== '' || titles.length > 0) &&
    (!needsLink || link.trim() !== '')
  const confirm = outcome === 'done' ? doneConfirm(task?.doneWhen) : undefined
  const title = task?.title
  return (
    <Frame
      title="Resolve task"
      onSubmit={() =>
        resolve.mutate(
          {
            id,
            outcome,
            note: note.trim() || undefined,
            link: needsLink ? link.trim() : undefined,
            followUps: outcome === 'done' || outcome === 'dropped' ? undefined : titles,
            doneWhen: changed ? dw.trim() : undefined,
          },
          {
            onSuccess: (r) => {
              const words = r.outcome === 'done' ? 'Finished' : `Closed as ${r.outcome}:`
              let text = `${words} ${title ? `"${title}"` : 'the task'}.`
              if (r.next) text += ` Next recurrence ${dateWords(r.next)}.`
              if (r.followUps.length) text += ` Made ${r.followUps.length} follow-up.`
              if (r.moved.length) text += ` ${r.moved.length} waiting item moved to it.`
              if (r.released.length) text += ` ${r.released.length} waiting item released.`
              onDone(text)
              close()
            },
          },
        )
      }
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={resolve.isPending || !ok}>
            {confirm ? 'Yes, done' : 'Resolve'}
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {resolve.isError && <Alert severity="error">{errorText(resolve.error)}</Alert>}
        {title && <Typography sx={{ overflowWrap: 'anywhere' }}>{title}</Typography>}
        {confirm && <Typography sx={{ overflowWrap: 'anywhere' }}>{confirm}</Typography>}
        <TextField
          select
          label="Outcome"
          value={outcome}
          onChange={(e) => setOutcome(e.target.value as Para_TaskResolveInputOutcome)}
          helperText={OUTCOMES.find((o) => o.value === outcome)?.hint}
          fullWidth
        >
          {OUTCOMES.map((o) => (
            <MenuItem key={o.value} value={o.value}>
              {o.label}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          label={
            outcome === 'dropped'
              ? 'Why it is not needed'
              : outcome === 'partial'
                ? 'What was done, and why the rest is not a follow-up'
                : 'Note (what was done or decided)'
          }
          value={note}
          onChange={(e) => setNote(e.target.value)}
          required={outcome === 'dropped'}
          multiline
          minRows={2}
          autoFocus
          fullWidth
        />
        {needsLink && (
          <TextField
            label={
              outcome === 'duplicate'
                ? 'Id of the item it duplicates'
                : 'Id of the item that replaces it'
            }
            value={link}
            onChange={(e) => setLink(e.target.value)}
            required
            fullWidth
          />
        )}
        {(outcome === 'partial' || needsLink) && (
          <TextField
            label="Follow-up tasks (one title per line, optional)"
            value={followUps}
            onChange={(e) => setFollowUps(e.target.value)}
            multiline
            minRows={2}
            helperText="They go in the same goal or area. Items that waited on this task move to the first."
            fullWidth
          />
        )}
        {task?.doneWhen && (
          <TextField
            label="Done when (change it if it was wrong; the old text is kept)"
            value={dw}
            onChange={(e) => setDoneWhen(e.target.value)}
            multiline
            fullWidth
          />
        )}
      </Stack>
    </Frame>
  )
}

function BlockDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const block = useTaskBlock()
  const title = useTask(id).data?.title
  const [reason, setReason] = useState('')
  return (
    <Frame
      title="Block task"
      onSubmit={() =>
        block.mutate(
          { id, reason: reason.trim() },
          {
            onSuccess: () => {
              onDone(`Blocked ${title ? `"${title}"` : 'the task'}.`)
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
            disabled={block.isPending || reason.trim() === ''}
          >
            Block
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {block.isError && <Alert severity="error">{errorText(block.error)}</Alert>}
        {title && <Typography sx={{ overflowWrap: 'anywhere' }}>{title}</Typography>}
        <TextField
          label="Why it is blocked (who said it, and where it applies)"
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

function AssignDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const assign = useTaskAssign()
  const task = useTask(id).data
  const [who, setWho] = useState<string | null>(null)
  const value = who ?? task?.assignee ?? ''
  return (
    <Frame
      title="Assign task"
      onSubmit={() =>
        assign.mutate(
          { id, assignee: value.trim() || 'self' },
          {
            onSuccess: () => {
              onDone(
                `Assigned ${task?.title ? `"${task.title}"` : 'the task'} to ${value.trim() || 'you'}.`,
              )
              close()
            },
          },
        )
      }
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={assign.isPending}>
            Assign
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {assign.isError && <Alert severity="error">{errorText(assign.error)}</Alert>}
        {task?.title && <Typography sx={{ overflowWrap: 'anywhere' }}>{task.title}</Typography>}
        <TextField
          label="Assign to (self, a person id, or agent:name)"
          value={value}
          onChange={(e) => setWho(e.target.value)}
          autoFocus
          fullWidth
        />
        <Typography variant="caption" color="text.secondary">
          Para stores the name and sends nothing. Use Give to agent to queue agent work.
        </Typography>
      </Stack>
    </Frame>
  )
}

function DelegateDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const delegate = useTaskDelegate()
  const task = useTask(id).data
  const working = useWorking().has(id)
  const [agent, setAgent] = useState('')
  const [hint, setHint] = useState('')
  return (
    <Frame
      title="Give to agent"
      onSubmit={() =>
        delegate.mutate(
          { id, agent: agent.trim() || undefined, hint: hint.trim() || undefined },
          {
            onSuccess: (r) => {
              onDone(
                `${r.appended ? 'Added your hint to the open agent work.' : 'Queued agent work.'}${r.note ? ` ${r.note}` : ''}`,
              )
              close()
            },
          },
        )
      }
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={delegate.isPending}>
            Give to agent
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {delegate.isError && <Alert severity="error">{errorText(delegate.error)}</Alert>}
        {task?.title && <Typography sx={{ overflowWrap: 'anywhere' }}>{task.title}</Typography>}
        <Typography variant="body2" color="text.secondary">
          {working
            ? 'An agent is working on it already. A hint goes into its work.'
            : 'Queues agent work linked to this task, with its done condition. The agent reports with sources and cannot act outside para without an approved proposal. You check what it reports.'}
        </Typography>
        <TextField
          label="Agent name (optional; default is the vault runner)"
          value={agent}
          onChange={(e) => setAgent(e.target.value)}
          fullWidth
        />
        <TextField
          label="Hint: where to start (optional)"
          value={hint}
          onChange={(e) => setHint(e.target.value)}
          multiline
          minRows={2}
          autoFocus
          fullWidth
        />
      </Stack>
    </Frame>
  )
}
