import { useState } from 'react'
import { useParams } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import {
  useAnnotate,
  useDeny,
  useEdit,
  useHold,
  useProposal,
  useRework,
  useSendback,
} from '@/api/proposals'
import { useAreas } from '@/api/areas'
import { useWork } from '@/api/work'
import { errorText } from '@/client/gqlClient'
import { Pick } from '@/components/Pick'
import { Frame, useClose } from '@/components/OverlayFrame'
import { parseDecide, type OverlaySearch } from '@/lib/overlays'
import { parseInputObject, quoteAnchors } from '@/lib/proposal'

type Props = { s: OverlaySearch; onDone: (m: string) => void }

// The proposal dialogs open only on /proposals/$id, where the route names the proposal.
export function ProposalDialogs({ s, onDone }: Props) {
  const { id } = useParams({ strict: false })
  if (!id) return null
  const decide = s.decide ? parseDecide(s.decide) : null
  return (
    <>
      {decide && <DecideDialog id={id} action={decide.action} item={decide.item} onDone={onDone} />}
      {s.note && <NoteDialog id={id} item={s.note} quote={s.quote ?? ''} onDone={onDone} />}
      {s.edit && <EditDialog id={id} item={s.edit} onDone={onDone} />}
    </>
  )
}

const ACTION_TITLE: Record<string, string> = {
  deny: 'Deny',
  rework: 'Rework',
  sendback: 'Send back',
  hold: 'Needs first',
}

function DecideDialog({
  id,
  action,
  item,
  onDone,
}: {
  id: string
  action: string
  item: string
  onDone: (m: string) => void
}) {
  const close = useClose()
  const deny = useDeny()
  const rework = useRework()
  const sendback = useSendback()
  const hold = useHold()
  const [reason, setReason] = useState('')
  const [kind, setKind] = useState<'task' | 'request'>('task')
  const [project, setProject] = useState('')
  const [area, setArea] = useState('')
  const work = useWork()
  const areas = useAreas()
  const m =
    action === 'deny'
      ? deny
      : action === 'rework'
        ? rework
        : action === 'sendback'
          ? sendback
          : action === 'hold'
            ? hold
            : null
  const title = ACTION_TITLE[action]
  if (!title || !m) return null
  const reasonRequired = action === 'deny' || action === 'rework'
  const ok = action === 'sendback' || reason.trim() !== ''
  const done = (msg: string) => {
    onDone(msg)
    close()
  }
  const submit = () => {
    const r = reason.trim()
    if (action === 'deny') deny.mutate({ id, item, reason: r }, { onSuccess: () => done('Denied') })
    else if (action === 'rework')
      rework.mutate({ id, item, reason: r }, { onSuccess: () => done('Returned for rework') })
    else if (action === 'sendback')
      sendback.mutate(
        { id, item, reason: r || undefined },
        { onSuccess: () => done('Sent back to the agent') },
      )
    else
      hold.mutate(
        {
          id,
          item,
          kind,
          text: r,
          project: kind === 'task' ? project.trim() || undefined : undefined,
          area: kind === 'task' ? area.trim() || undefined : undefined,
        },
        { onSuccess: () => done('Held until it closes') },
      )
  }
  return (
    <Frame
      title={`${title} ${item}`}
      onSubmit={submit}
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={m.isPending || !ok}>
            {title}
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {m.isError && <Alert severity="error">{errorText(m.error)}</Alert>}
        {action === 'hold' && (
          <TextField
            select
            label="Held on a new"
            value={kind}
            onChange={(e) => setKind(e.target.value as 'task' | 'request')}
          >
            <MenuItem value="task">Task</MenuItem>
            <MenuItem value="request">Request for an agent</MenuItem>
          </TextField>
        )}
        <TextField
          label={action === 'hold' ? (kind === 'task' ? 'Task title' : 'Ask') : 'Reason'}
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          required={action !== 'sendback'}
          multiline
          minRows={2}
          autoFocus
          fullWidth
          helperText={
            reasonRequired
              ? 'Required. The agent reads it.'
              : action === 'sendback'
                ? 'Optional. Your edits and notes go with it.'
                : undefined
          }
        />
        {action === 'hold' && kind === 'task' && (
          <>
            <Pick
              label="Project (optional)"
              value={project}
              onChange={setProject}
              options={work.data?.projects ?? []}
              none="No project"
            />
            <Pick
              label="Area (optional)"
              value={area}
              onChange={setArea}
              options={areas.data ?? []}
              none="No area"
            />
          </>
        )}
      </Stack>
    </Frame>
  )
}

function NoteDialog({
  id,
  item,
  quote: initial,
  onDone,
}: {
  id: string
  item: string
  quote: string
  onDone: (m: string) => void
}) {
  const close = useClose()
  const annotate = useAnnotate()
  const p = useProposal(id)
  const [quote, setQuote] = useState(initial)
  const [note, setNote] = useState('')
  const it = p.data?.items.find((x) => x.id === item)
  // Wait for the item: until it is loaded the anchor cannot be judged.
  const anchored = it ? quoteAnchors(it, quote) : true
  return (
    <Frame
      title={`Note on ${item}`}
      onSubmit={() =>
        annotate.mutate(
          { id, item, quote: quote.trim(), note: note.trim() },
          {
            onSuccess: () => {
              onDone('Note added. The item can now be sent back.')
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
            disabled={annotate.isPending || quote.trim() === '' || note.trim() === ''}
          >
            Add note
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {annotate.isError && <Alert severity="error">{errorText(annotate.error)}</Alert>}
        <TextField
          label="Quoted text"
          value={quote}
          onChange={(e) => setQuote(e.target.value)}
          required
          multiline
          minRows={1}
          fullWidth
          error={!anchored}
          helperText={
            anchored
              ? 'Select text on the item before you press Note, or type the words here.'
              : 'This text is not in the item. The note will not anchor.'
          }
        />
        <TextField
          label="Your note"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          required
          multiline
          minRows={3}
          autoFocus
          fullWidth
        />
        <Typography variant="caption" color="text.secondary">
          The agent must answer every note: applied, or declined with a reason. An item with a note
          can only be sent back.
        </Typography>
      </Stack>
    </Frame>
  )
}

function EditDialog({
  id,
  item,
  onDone,
}: {
  id: string
  item: string
  onDone: (m: string) => void
}) {
  const p = useProposal(id)
  const it = p.data?.items.find((x) => x.id === item)
  if (!it) {
    return (
      <Frame title={`Edit ${item}`} actions={<CloseOnly />}>
        <Typography>{p.isError ? errorText(p.error) : 'Loading'}</Typography>
      </Frame>
    )
  }
  return <EditForm id={id} item={item} it={it} onDone={onDone} />
}

function CloseOnly() {
  const close = useClose()
  return <Button onClick={close}>Close</Button>
}

type EditItem = {
  kind: string
  draft?: string | null
  target?: string | null
  input?: unknown
}

function EditForm({
  id,
  item,
  it,
  onDone,
}: {
  id: string
  item: string
  it: EditItem
  onDone: (m: string) => void
}) {
  const close = useClose()
  const edit = useEdit()
  const external = it.kind === 'external'
  const [draft, setDraft] = useState(it.draft ?? '')
  const [target, setTarget] = useState(it.target ?? '')
  const [input, setInput] = useState(JSON.stringify(it.input ?? {}, null, 2))
  const parsed = external ? null : parseInputObject(input)
  const changed = external
    ? draft !== (it.draft ?? '') || target !== (it.target ?? '')
    : input !== JSON.stringify(it.input ?? {}, null, 2)
  const ok = changed && (parsed === null || parsed.ok)
  const submit = () => {
    const done = () => {
      onDone('Edited. Send it back, or approve it as edited.')
      close()
    }
    if (external) {
      edit.mutate(
        {
          id,
          item,
          draft: draft !== (it.draft ?? '') ? draft : undefined,
          target: target !== (it.target ?? '') ? target : undefined,
        },
        { onSuccess: done },
      )
    } else if (parsed?.ok) {
      edit.mutate({ id, item, input: parsed.value }, { onSuccess: done })
    }
  }
  return (
    <Frame
      title={`Edit ${item}`}
      onSubmit={submit}
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={edit.isPending || !ok}>
            Save edit
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {edit.isError && <Alert severity="error">{errorText(edit.error)}</Alert>}
        {external ? (
          <>
            <TextField
              label="Target"
              value={target}
              onChange={(e) => setTarget(e.target.value)}
              fullWidth
            />
            <TextField
              label="Draft"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              multiline
              minRows={6}
              autoFocus
              fullWidth
            />
          </>
        ) : (
          <TextField
            label="Input (JSON, in full)"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            multiline
            minRows={6}
            autoFocus
            fullWidth
            error={parsed !== null && !parsed.ok}
            helperText={parsed && !parsed.ok ? parsed.error : undefined}
            slotProps={{ htmlInput: { style: { fontFamily: 'ui-monospace, monospace' } } }}
          />
        )}
        <Typography variant="caption" color="text.secondary">
          An edited item can be sent back to the agent, or approved as edited. Approval binds to
          your text.
        </Typography>
      </Stack>
    </Frame>
  )
}
