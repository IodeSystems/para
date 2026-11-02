import { useState, type ReactNode } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import MenuItem from '@mui/material/MenuItem'
import Snackbar from '@mui/material/Snackbar'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useCapture, useInboxItem, useTriage } from '@/api/inbox'
import { useRequestAdd } from '@/api/requests'
import { useRfxReply } from '@/api/rfx'
import { errorText } from '@/client/gqlClient'
import { NO_OVERLAYS } from '@/lib/overlays'
import { TABS } from '@/lib/keys'
import type { Para_InboxTriageInputTo } from '@/gql/graphql'

// Every overlay is a search param on the current route (WEB-10). Closing drops the params in
// place with replace, so Back never re-opens one.
function useClose() {
  const navigate = useNavigate()
  return () =>
    void navigate({ to: '.', search: (prev) => ({ ...prev, ...NO_OVERLAYS }), replace: true })
}

export function Overlays() {
  const s = useSearch({ strict: false })
  const [toast, setToast] = useState('')
  return (
    <>
      {s.capture && <CaptureDialog onDone={setToast} />}
      {s.request && <RequestDialog onDone={setToast} />}
      {s.help && <HelpDialog />}
      {s.triage && <TriageDialog id={s.triage} onDone={setToast} />}
      {s.reply && <ReplyDialog id={s.reply} onDone={setToast} />}
      <Snackbar
        open={toast !== ''}
        autoHideDuration={3000}
        onClose={() => setToast('')}
        message={toast}
      />
    </>
  )
}

function Frame({
  title,
  children,
  actions,
  onSubmit,
}: {
  title: string
  children: ReactNode
  actions: ReactNode
  onSubmit?: () => void
}) {
  const close = useClose()
  return (
    <Dialog open onClose={close} fullWidth maxWidth="sm" aria-labelledby="overlay-title">
      <form
        onSubmit={(e) => {
          e.preventDefault()
          onSubmit?.()
        }}
      >
        <DialogTitle id="overlay-title">{title}</DialogTitle>
        <DialogContent>{children}</DialogContent>
        <DialogActions>{actions}</DialogActions>
      </form>
    </Dialog>
  )
}

function CaptureDialog({ onDone }: { onDone: (m: string) => void }) {
  const close = useClose()
  const cap = useCapture()
  const [text, setText] = useState('')
  const submit = () => {
    if (text.trim() === '') return
    cap.mutate(text.trim(), {
      onSuccess: () => {
        onDone('Captured to the inbox')
        close()
      },
    })
  }
  return (
    <Frame
      title="Capture"
      onSubmit={submit}
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={cap.isPending || text.trim() === ''}>
            Capture
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {cap.isError && <Alert severity="error">{errorText(cap.error)}</Alert>}
        <TextField
          label="What is on your mind?"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) submit()
          }}
          multiline
          minRows={3}
          autoFocus
          fullWidth
        />
        <Typography variant="caption" color="text.secondary">
          Goes to the inbox. Sort it later. Ctrl+Enter captures.
        </Typography>
      </Stack>
    </Frame>
  )
}

function RequestDialog({ onDone }: { onDone: (m: string) => void }) {
  const close = useClose()
  const add = useRequestAdd()
  const [ask, setAsk] = useState('')
  return (
    <Frame
      title="Request for an agent"
      onSubmit={() =>
        add.mutate(ask.trim(), {
          onSuccess: () => {
            onDone('Request queued')
            close()
          },
        })
      }
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={add.isPending || ask.trim() === ''}>
            Queue
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {add.isError && <Alert severity="error">{errorText(add.error)}</Alert>}
        <TextField
          label="Ask"
          value={ask}
          onChange={(e) => setAsk(e.target.value)}
          multiline
          minRows={3}
          autoFocus
          fullWidth
        />
        <Typography variant="caption" color="text.secondary">
          Para starts no agent. An agent that polls the queue claims it and its result lands in
          Review.
        </Typography>
      </Stack>
    </Frame>
  )
}

function HelpDialog() {
  const close = useClose()
  return (
    <Dialog open onClose={close} fullWidth maxWidth="xs" aria-labelledby="help-title">
      <DialogTitle id="help-title">Keyboard</DialogTitle>
      <DialogContent>
        <Stack component="dl" spacing={0.5} sx={{ m: 0 }}>
          {TABS.map((t) => (
            <Row key={t.key} k={t.key} v={t.label} />
          ))}
          <Row k="/" v="Search" />
          <Row k="c" v="Capture" />
          <Row k="?" v="This help" />
          <Row k="Esc" v="Close a dialog" />
        </Stack>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 2 }}>
          Keys do nothing while you type in a field.
        </Typography>
      </DialogContent>
      <DialogActions>
        <Button onClick={close}>Close</Button>
      </DialogActions>
    </Dialog>
  )
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <Stack direction="row" spacing={2} sx={{ alignItems: 'baseline' }}>
      <Typography component="dt" sx={{ fontFamily: 'ui-monospace, monospace', width: 40 }}>
        {k}
      </Typography>
      <Typography component="dd" sx={{ m: 0 }}>
        {v}
      </Typography>
    </Stack>
  )
}

const TRIAGE_TARGETS: { value: Para_InboxTriageInputTo; label: string }[] = [
  { value: 'task', label: 'Task' },
  { value: 'project', label: 'Project' },
  { value: 'rfx', label: 'RFX (ask someone)' },
  { value: 'note', label: 'Note on a person' },
  { value: 'drop', label: 'Drop (archive)' },
]

function TriageDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const item = useInboxItem(id)
  const triage = useTriage()
  const [to, setTo] = useState<Para_InboxTriageInputTo>('task')
  const [project, setProject] = useState('')
  const [person, setPerson] = useState('')
  const [route, setRoute] = useState('')
  const submit = () =>
    triage.mutate(
      {
        id,
        to,
        project: to === 'task' ? project : undefined,
        person: to === 'note' ? person : undefined,
        route: to === 'rfx' ? route : undefined,
      },
      {
        onSuccess: (r) => {
          onDone(r.warnings.length ? `Sorted. ${r.warnings.join(' ')}` : `Sorted into ${r.to}`)
          close()
        },
      },
    )
  return (
    <Frame
      title="Sort inbox item"
      onSubmit={submit}
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={triage.isPending}>
            Sort
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        <Typography sx={{ whiteSpace: 'pre-wrap' }}>
          {item.data?.title ?? (item.isError ? errorText(item.error) : 'Loading')}
        </Typography>
        {triage.isError && <Alert severity="error">{errorText(triage.error)}</Alert>}
        <TextField
          select
          label="Becomes"
          value={to}
          onChange={(e) => setTo(e.target.value as Para_InboxTriageInputTo)}
        >
          {TRIAGE_TARGETS.map((t) => (
            <MenuItem key={t.value} value={t.value}>
              {t.label}
            </MenuItem>
          ))}
        </TextField>
        {to === 'task' && (
          <TextField
            label="Project id (optional)"
            value={project}
            onChange={(e) => setProject(e.target.value)}
          />
        )}
        {to === 'note' && (
          <TextField
            label="Person id"
            value={person}
            onChange={(e) => setPerson(e.target.value)}
            required
          />
        )}
        {to === 'rfx' && (
          <TextField
            label="Route (who or where to ask)"
            value={route}
            onChange={(e) => setRoute(e.target.value)}
          />
        )}
      </Stack>
    </Frame>
  )
}

function ReplyDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const reply = useRfxReply()
  const [from, setFrom] = useState('')
  const [outcome, setOutcome] = useState<'answered' | 'partial' | 'no'>('answered')
  const [text, setText] = useState('')
  const [why, setWhy] = useState('')
  const ok = from.trim() !== '' && text.trim() !== '' && (outcome !== 'no' || why.trim() !== '')
  return (
    <Frame
      title={`Record a reply to ${id}`}
      onSubmit={() =>
        reply.mutate(
          { id, from: from.trim(), outcome, text: text.trim(), why: why.trim() },
          {
            onSuccess: (r) => {
              onDone(
                r.constraint
                  ? `Reply recorded. Constraint ${r.constraint} created.`
                  : 'Reply recorded',
              )
              close()
            },
          },
        )
      }
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={reply.isPending || !ok}>
            Record
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {reply.isError && <Alert severity="error">{errorText(reply.error)}</Alert>}
        <TextField
          label="From (person id or name)"
          value={from}
          onChange={(e) => setFrom(e.target.value)}
          required
          autoFocus
        />
        <TextField
          select
          label="Outcome"
          value={outcome}
          onChange={(e) => setOutcome(e.target.value as typeof outcome)}
        >
          <MenuItem value="answered">Answered</MenuItem>
          <MenuItem value="partial">Partly answered</MenuItem>
          <MenuItem value="no">No (creates a constraint)</MenuItem>
        </TextField>
        <TextField
          label="What they said"
          value={text}
          onChange={(e) => setText(e.target.value)}
          multiline
          minRows={3}
          required
        />
        {outcome === 'no' && (
          <TextField label="Why" value={why} onChange={(e) => setWhy(e.target.value)} required />
        )}
      </Stack>
    </Frame>
  )
}
