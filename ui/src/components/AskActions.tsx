import { useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import Alert from '@mui/material/Alert'
import Autocomplete from '@mui/material/Autocomplete'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import FormControlLabel from '@mui/material/FormControlLabel'
import MenuItem from '@mui/material/MenuItem'
import Popover from '@mui/material/Popover'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import { rfxPathAdd, rfxReply } from '@/api/askActions'
import { doneWithNote } from '@/api/noted'
import { useRfxPaths, REASONS } from '@/api/paths'
import { useGroups, usePeople } from '@/api/people'
import { useRfx } from '@/api/rfx'
import { localDate } from '@/api/today'
import { closeAny } from '@/api/verbs'
import { errorText } from '@/client/gqlClient'
import { useToasts } from '@/components/ToastStack'
import {
  HEARD,
  MEDIUMS,
  droppedNote,
  httpUrl,
  needsLink,
  openTargets,
  settlesAsk,
  targetKey,
} from '@/lib/ask'

// The owner settles an Ask by hand when the answer came outside para's sweep: Answered (a reply
// recorded for one target; the Ask is answered when no target is left), Dropped (the Ask closes,
// the reason goes to its notes), Rerouted (a next move to another person). Each is a small
// popover with Back and a confirm button.

const WIDTH = { xs: 'calc(100vw - 32px)', sm: 340 }

function Pop({
  at,
  onClose,
  children,
}: {
  at: HTMLElement | null
  onClose: () => void
  children: ReactNode
}) {
  return (
    <Popover anchorEl={at} open={!!at} onClose={onClose}>
      <Box
        sx={{ p: { xs: 1.5, sm: 2 }, width: WIDTH, maxWidth: 340 }}
        onKeyDown={(e) => e.stopPropagation()}
      >
        <Stack spacing={1.5}>{children}</Stack>
      </Box>
    </Popover>
  )
}

function Buttons({
  onBack,
  label,
  act,
  disabled,
  onConfirm,
}: {
  onBack: () => void
  label: string
  act: string
  disabled: boolean
  onConfirm: () => void
}) {
  return (
    <Box>
      <Button size="small" sx={{ mr: 1 }} onClick={onBack}>
        Back
      </Button>
      <Button
        size="small"
        variant="contained"
        data-act={act}
        disabled={disabled}
        onClick={onConfirm}
      >
        {label}
      </Button>
    </Box>
  )
}

function useSettle(id: string) {
  const toasts = useToasts()
  const qc = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const run = async (call: () => Promise<void>): Promise<boolean> => {
    setBusy(true)
    setError('')
    try {
      await call()
      await qc.invalidateQueries()
      return true
    } catch (e) {
      setError(errorText(e))
      return false
    } finally {
      setBusy(false)
    }
  }
  return { toasts, qc, busy, error, setError, run, id }
}

// AnswerButton records an answer the owner got: who answered (one of the targets that have not
// replied), what they said, where it was heard, a link (a no needs one), and the date.
export function AnswerButton({
  id,
  title,
  label = 'Answered',
  fallback,
}: {
  id: string
  title: string
  label?: string
  // fallback is the plain close, for an Ask that takes no reply (an inbound one).
  fallback?: () => void
}) {
  const q = useRfx(id)
  const s = useSettle(id)
  const [at, setAt] = useState<HTMLElement | null>(null)
  const [who, setWho] = useState('')
  const [outcome, setOutcome] = useState('answered')
  const [text, setText] = useState('')
  const [heard, setHeard] = useState('')
  const [url, setUrl] = useState('')
  const [date, setDate] = useState(localDate())
  const r = q.data
  const targets = r?.doc.meta.targets ?? []
  const open = openTargets(targets)
  const names = new Map(targets.map((t, i) => [targetKey(t), r?.targetNames?.[i] || t.to]))
  const key = open.some((t) => targetKey(t) === who) ? who : open[0] ? targetKey(open[0]) : ''
  const target = open.find((t) => targetKey(t) === key)
  const outbound = r?.doc.meta.direction !== 'in'
  const link = url.trim()
  const linkOk = !link ? !needsLink(outcome, heard) || !!target?.ref : httpUrl(link)
  const linkNeeded = needsLink(outcome, heard) && !link && !target?.ref
  if (r && (!outbound || open.length === 0) && fallback) {
    return (
      <Button size="small" data-act="done" onClick={fallback}>
        {label}
      </Button>
    )
  }
  if (r && (!outbound || open.length === 0)) return null
  const confirm = async () => {
    if (!target) return
    const ok = await s.run(async () => {
      await rfxReply({
        id,
        from: target.to,
        via: target.medium,
        outcome,
        text: text.trim(),
        heard: heard || undefined,
        url: link || undefined,
        date: date || undefined,
        why: 'The owner heard it outside para',
      })
      // Every target has replied: the Ask is answered. A close with outcome answered files a
      // receipt, but the reply has none, so no Undo is offered.
      if (settlesAsk(targets, key)) await closeAny({ id, outcome: 'answered' })
    })
    if (!ok) return
    setAt(null)
    setText('')
    setUrl('')
    s.toasts.push({
      text: `Answered: ${title || id}`,
      target: { id, kind: 'rfx' },
      note: 'A recorded reply has no Undo.',
    })
  }
  return (
    <>
      <Button
        size="small"
        data-act="answered"
        disabled={!r}
        onClick={(e) => setAt(e.currentTarget)}
      >
        {label}
      </Button>
      <Pop at={at} onClose={() => setAt(null)}>
        {open.length > 1 && (
          <TextField
            select
            size="small"
            label="Who answered"
            value={key}
            onChange={(e) => setWho(e.target.value)}
          >
            {open.map((t) => (
              <MenuItem key={targetKey(t)} value={targetKey(t)}>
                {names.get(targetKey(t))} ({t.medium})
              </MenuItem>
            ))}
          </TextField>
        )}
        {open.length === 1 && <Box sx={{ fontSize: 14 }}>Answered by {names.get(key)}</Box>}
        <TextField
          select
          size="small"
          label="Answer"
          value={outcome}
          onChange={(e) => setOutcome(e.target.value)}
        >
          <MenuItem value="answered">Answered</MenuItem>
          <MenuItem value="partial">Partly answered</MenuItem>
          <MenuItem value="no">No</MenuItem>
        </TextField>
        <TextField
          multiline
          minRows={2}
          fullWidth
          autoFocus
          size="small"
          label={
            outcome === 'no' ? 'What they said, and why (required)' : 'What they said (required)'
          }
          value={text}
          onChange={(e) => setText(e.target.value)}
        />
        <TextField
          select
          size="small"
          label="Where you heard it"
          value={heard}
          onChange={(e) => setHeard(e.target.value)}
        >
          {HEARD.map((h) => (
            <MenuItem key={h.code} value={h.code}>
              {h.label}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          size="small"
          label={
            needsLink(outcome, heard)
              ? 'Link to the reply (required for a no, or say where you heard it)'
              : 'Link (optional)'
          }
          value={url}
          error={!!link && !httpUrl(link)}
          onChange={(e) => setUrl(e.target.value)}
        />
        <TextField
          size="small"
          type="date"
          label="Date"
          value={date}
          onChange={(e) => setDate(e.target.value)}
          slotProps={{ inputLabel: { shrink: true } }}
        />
        {linkNeeded && (
          <Alert severity="info">
            A no is kept as a constraint with its source. Give a link to where it was said, or say
            where you heard it.
          </Alert>
        )}
        {s.error && <Alert severity="error">{s.error}</Alert>}
        <Buttons
          onBack={() => setAt(null)}
          label="Mark answered"
          act="answered-confirm"
          disabled={s.busy || !target || !text.trim() || !linkOk}
          onConfirm={() => void confirm()}
        />
      </Pop>
    </>
  )
}

// DropButton closes the Ask with the owner's reason, which goes to its notes. Undo opens it again
// and takes the note out. An inbound Ask closes only with a playbook: the server's refusal shows.
export function DropButton({ id, title }: { id: string; title: string }) {
  const s = useSettle(id)
  const qc = useQueryClient()
  const [at, setAt] = useState<HTMLElement | null>(null)
  const [text, setText] = useState('')
  const confirm = async () => {
    let undo: (() => Promise<void>) | undefined
    const ok = await s.run(async () => {
      const r = await doneWithNote({
        id,
        done: 'ask',
        text: droppedNote(text),
        files: [],
      })
      undo = r.undo
    })
    if (!ok) return
    setAt(null)
    setText('')
    s.toasts.push({
      text: `Dropped: ${title || id}`,
      target: { id, kind: 'rfx' },
      undo: undo
        ? async () => {
            await undo?.()
            await qc.invalidateQueries()
          }
        : undefined,
      note: undo
        ? 'Undo opens it again and takes the note out.'
        : 'This close left no receipt to undo.',
    })
  }
  return (
    <>
      <Button size="small" data-act="dropped" onClick={(e) => setAt(e.currentTarget)}>
        Dropped
      </Button>
      <Pop at={at} onClose={() => setAt(null)}>
        <TextField
          multiline
          minRows={2}
          fullWidth
          autoFocus
          size="small"
          label="Why it is dropped (required)"
          value={text}
          onChange={(e) => setText(e.target.value)}
        />
        {s.error && <Alert severity="error">{s.error}</Alert>}
        <Buttons
          onBack={() => setAt(null)}
          label="Drop"
          act="dropped-confirm"
          disabled={s.busy || !text.trim()}
          onConfirm={() => void confirm()}
        />
      </Pop>
    </>
  )
}

type Person = { id: string; title: string; group?: boolean }

// RerouteButton moves the Ask to another person: a new target with a reason code and a reason.
// Para sends nothing. A person held back (away, quiet days) is refused unless the owner overrides.
export function RerouteButton({ id, title }: { id: string; title: string }) {
  const s = useSettle(id)
  const people = usePeople()
  const groups = useGroups()
  const options: Person[] = [
    ...(people.data ?? []),
    ...(groups.data ?? []).map((g) => ({ ...g, group: true })),
  ]
  const paths = useRfxPaths(id)
  const [at, setAt] = useState<HTMLElement | null>(null)
  const [to, setTo] = useState<Person | null>(null)
  const [medium, setMedium] = useState('email')
  const [reason, setReason] = useState('wrong_owner')
  const [why, setWhy] = useState('')
  const [override, setOverride] = useState(false)
  const suggestions = (paths.data?.moves ?? []).filter((m) => m.to && !m.hold)
  const held = /held back/.test(s.error)
  const pick = (m: { to?: string | null; toName?: string | null; medium?: string | null }) => {
    if (!m.to) return
    setTo({ id: m.to, title: m.toName || m.to })
    if (m.medium && (MEDIUMS as readonly string[]).includes(m.medium)) setMedium(m.medium)
  }
  const confirm = async () => {
    if (!to) return
    const ok = await s.run(async () => {
      await rfxPathAdd({
        id,
        to: to.id,
        medium,
        reason,
        why: why.trim(),
        override: override || undefined,
      })
    })
    if (!ok) return
    setAt(null)
    setWhy('')
    setTo(null)
    setOverride(false)
    s.toasts.push({
      text: `Rerouted to ${to.title}: ${title || id}`,
      target: { id, kind: 'rfx' },
      note: 'Para sent nothing. A move has no Undo.',
    })
  }
  return (
    <>
      <Button size="small" data-act="rerouted" onClick={(e) => setAt(e.currentTarget)}>
        Rerouted
      </Button>
      <Pop at={at} onClose={() => setAt(null)}>
        <Autocomplete<Person>
          options={options}
          groupBy={(o) => (o.group ? 'Groups' : 'People')}
          value={to}
          onChange={(_, v) => setTo(v)}
          getOptionLabel={(o) => o.title || o.id}
          isOptionEqualToValue={(a, b) => a.id === b.id}
          renderInput={(p) => (
            <TextField {...p} size="small" label="Rerouted to (a person or a group)" autoFocus />
          )}
        />
        {suggestions.length > 0 && (
          <Stack direction="row" useFlexGap sx={{ flexWrap: 'wrap', gap: 0.5 }}>
            {suggestions.slice(0, 4).map((m, i) => (
              <Button key={`${m.to}-${i}`} size="small" variant="outlined" onClick={() => pick(m)}>
                {m.toName || m.to}
              </Button>
            ))}
          </Stack>
        )}
        <TextField
          select
          size="small"
          label="By"
          value={medium}
          onChange={(e) => setMedium(e.target.value)}
        >
          {MEDIUMS.map((m) => (
            <MenuItem key={m} value={m}>
              {m}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          select
          size="small"
          label="Reason"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
        >
          {REASONS.map((r) => (
            <MenuItem key={r.code} value={r.code}>
              {r.label}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          multiline
          minRows={2}
          fullWidth
          size="small"
          label="Why it moves (required)"
          value={why}
          onChange={(e) => setWhy(e.target.value)}
        />
        {s.error && <Alert severity="error">{s.error}</Alert>}
        {held && (
          <FormControlLabel
            control={
              <Checkbox
                size="small"
                checked={override}
                onChange={(e) => setOverride(e.target.checked)}
              />
            }
            label="Ask anyway"
          />
        )}
        <Buttons
          onBack={() => setAt(null)}
          label="Reroute"
          act="rerouted-confirm"
          disabled={s.busy || !to || !why.trim()}
          onConfirm={() => void confirm()}
        />
      </Pop>
    </>
  )
}

// AskActions are the three buttons on an open Ask's page.
export function AskActions({ id, title }: { id: string; title: string }) {
  return (
    <Stack
      direction="row"
      useFlexGap
      sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 0.5, mb: 2 }}
      data-ask-actions={id}
    >
      <AnswerButton id={id} title={title} />
      <DropButton id={id} title={title} />
      <RerouteButton id={id} title={title} />
    </Stack>
  )
}
