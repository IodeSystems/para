import { useState } from 'react'
import { useParams } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import type { Para_ProposalRequestInputIntent } from '@/gql/graphql'
import { useItemRequest, useDeny, useEdit, useHold, useProposal } from '@/api/proposals'
import { useAreas } from '@/api/areas'
import { useWork } from '@/api/work'
import { errorText } from '@/client/gqlClient'
import { Pick } from '@/components/Pick'
import { Frame, useClose } from '@/components/OverlayFrame'
import { parseDecide, type OverlaySearch } from '@/lib/overlays'
import { isOutward, parseInputObject } from '@/lib/proposal'

type Props = { s: OverlaySearch; onDone: (m: string) => void }

// The proposal dialogs open only on /proposals/$id, where the route names the proposal.
export function ProposalDialogs({ s, onDone }: Props) {
  const { id } = useParams({ strict: false })
  if (!id) return null
  const decide = s.decide ? parseDecide(s.decide) : null
  return (
    <>
      {decide && <DecideDialog id={id} action={decide.action} item={decide.item} onDone={onDone} />}
      {s.itemreq && <RequestDialog id={id} item={s.itemreq} onDone={onDone} />}
      {s.edit && <EditDialog id={id} item={s.edit} onDone={onDone} />}
    </>
  )
}

const ACTION_TITLE: Record<string, string> = {
  deny: 'Deny',
  hold: 'Needs first',
}

function DecideDialog({
  id,
  action: asked,
  item,
  onDone,
}: {
  id: string
  action: string
  item: string
  onDone: (m: string) => void
}) {
  const action = asked
  const p = useProposal(id)
  const it = p.data?.items.find((x) => x.id === item)
  const close = useClose()
  const deny = useDeny()
  const hold = useHold()
  const [reason, setReason] = useState('')
  const [kind, setKind] = useState<'task' | 'request'>('task')
  const [project, setProject] = useState('')
  const [area, setArea] = useState('')
  const work = useWork()
  const areas = useAreas()
  const m = action === 'deny' ? deny : action === 'hold' ? hold : null
  const title = ACTION_TITLE[action]
  if (!title || !m) return null
  const reasonRequired = action === 'deny'
  const ok = !reasonRequired && action !== 'hold' ? true : reason.trim() !== ''
  const done = (msg: string) => {
    onDone(msg)
    close()
  }
  const submit = () => {
    const r = reason.trim()
    if (action === 'deny') deny.mutate({ id, item, reason: r }, { onSuccess: () => done('Denied') })
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
      title={`${title}: ${it?.summary ?? 'this item'}`}
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
            <MenuItem value="request">Agent work</MenuItem>
          </TextField>
        )}
        <TextField
          label={action === 'hold' ? (kind === 'task' ? 'Task title' : 'Ask') : 'Reason'}
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          required={reasonRequired || action === 'hold'}
          multiline
          minRows={2}
          autoFocus
          fullWidth
          helperText={
            reasonRequired ? 'Required. The agent reads it.' : undefined
          }
        />
        {action === 'hold' && kind === 'task' && (
          <>
            <Pick
              label="Goal (optional)"
              value={project}
              onChange={setProject}
              options={work.data?.projects ?? []}
              none="No goal"
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

const REQUEST_TYPES: { value: Para_ProposalRequestInputIntent; label: string; help: string }[] = [
  { value: 'change', label: 'Changes', help: 'The agent revises the item as you say.' },
  { value: 'clarify', label: 'Clarification', help: 'The agent answers your question in a revision.' },
  {
    value: 'investigate',
    label: 'Investigation',
    help: 'The agent looks into it, and answers in a revision with sources.',
  },
]

function RequestDialog({
  id,
  item,
  onDone,
}: {
  id: string
  item: string
  onDone: (m: string) => void
}) {
  const close = useClose()
  const request = useItemRequest()
  const p = useProposal(id)
  const [type, setType] = useState<Para_ProposalRequestInputIntent>('change')
  const [text, setText] = useState('')
  const it = p.data?.items.find((x) => x.id === item)
  return (
    <Frame
      title={`Request on: ${it?.summary ?? 'this item'}`}
      onSubmit={() =>
        request.mutate(
          { id, item, note: text.trim(), intent: type },
          {
            onSuccess: () => {
              onDone('Requested. The agent will answer in a revision.')
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
            disabled={request.isPending || (type === 'change' && text.trim() === '')}
          >
            Request
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {request.isError && <Alert severity="error">{errorText(request.error)}</Alert>}
        <TextField
          select
          label="Type"
          value={type}
          onChange={(e) => setType(e.target.value as Para_ProposalRequestInputIntent)}
          helperText={REQUEST_TYPES.find((t) => t.value === type)?.help}
        >
          {REQUEST_TYPES.map((t) => (
            <MenuItem key={t.value} value={t.value}>
              {t.label}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          label={type === 'change' ? 'What to change' : 'Anything to add (optional)'}
          value={text}
          onChange={(e) => setText(e.target.value)}
          required={type === 'change'}
          multiline
          minRows={3}
          autoFocus
          fullWidth
        />
        <Typography variant="caption" color="text.secondary">
          The agent must answer every request: applied, or declined with a reason. An item with an
          open request cannot be approved.
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
      <Frame title="Edit" actions={<CloseOnly />}>
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
  summary?: string | null
  kind: string
  body?: string | null
  target?: string | null
  title?: string | null
  subject?: string | null
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
  const external = isOutward(it.kind)
  const [draft, setDraft] = useState(it.body ?? '')
  const [target, setTarget] = useState(it.target ?? '')
  const [title, setTitle] = useState(it.title ?? '')
  const [subject, setSubject] = useState(it.subject ?? '')
  const isAction = it.kind === 'action'
  const [input, setInput] = useState(JSON.stringify(it.input ?? {}, null, 2))
  const parsed = external ? null : parseInputObject(input)
  const changed = external
    ? draft !== (it.body ?? '') ||
      target !== (it.target ?? '') ||
      (isAction ? title !== (it.title ?? '') : subject !== (it.subject ?? ''))
    : input !== JSON.stringify(it.input ?? {}, null, 2)
  const ok = changed && (parsed === null || parsed.ok)
  const submit = () => {
    const done = () => {
      onDone('Edited. Approve it as edited, or request more.')
      close()
    }
    if (external) {
      edit.mutate(
        {
          id,
          item,
          body: draft !== (it.body ?? '') ? draft : undefined,
          target: target !== (it.target ?? '') ? target : undefined,
          title: isAction && title !== (it.title ?? '') ? title : undefined,
          subject: !isAction && subject !== (it.subject ?? '') ? subject : undefined,
        },
        { onSuccess: done },
      )
    } else if (parsed?.ok) {
      edit.mutate({ id, item, input: parsed.value }, { onSuccess: done })
    }
  }
  return (
    <Frame
      title={`Edit: ${it.summary ?? 'this item'}`}
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
            {isAction ? (
              <TextField
                label="Title"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                fullWidth
              />
            ) : (
              <TextField
                label="Subject"
                value={subject}
                onChange={(e) => setSubject(e.target.value)}
                fullWidth
              />
            )}
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
          Change the text yourself before approving. Approval binds to your text.
        </Typography>
      </Stack>
    </Frame>
  )
}
