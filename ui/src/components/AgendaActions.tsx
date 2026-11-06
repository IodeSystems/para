import { useState, type ReactNode } from 'react'
import { useRouteContext } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Collapse from '@mui/material/Collapse'
import Popover from '@mui/material/Popover'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import { readNotes, saveNotes } from '@/api/notes'
import { revertReceipt } from '@/api/receipts'
import { localDate } from '@/api/today'
import { closeAny, laterAny, reopenAny } from '@/api/verbs'
import { canDecide } from '@/client/session'
import { errorText } from '@/client/gqlClient'
import { DoItTask } from '@/components/DoIt'
import { LinkButton } from '@/components/links'
import { LaterButton } from '@/components/stack/LaterButton'
import { useToasts } from '@/components/ToastStack'
import { BriefBody } from '@/components/MeetingBrief'
import { agendaActions, closeBody, doneLabel, openTarget, type DoneKind } from '@/lib/agenda'
import { briefOpensItself } from '@/lib/brief'
import { notesWithEntry } from '@/lib/composer'
import { prefersReducedMotion } from '@/lib/toasts'
import { failText, toastText } from '@/lib/stackToast'

type Row = { id: string; title: string; kind?: string | null }

function OpenButton({ x }: { x: Row }) {
  switch (openTarget(x.kind)) {
    case 'incident':
      return (
        <LinkButton size="small" data-act="open" to="/incidents/$id" params={{ id: x.id }}>
          Open
        </LinkButton>
      )
    case 'rfx':
      return (
        <LinkButton size="small" data-act="open" to="/rfx/$id" params={{ id: x.id }}>
          Open
        </LinkButton>
      )
    case 'io':
      return (
        <LinkButton size="small" data-act="open" to="/io/$id" params={{ id: x.id }}>
          Open
        </LinkButton>
      )
    default:
      return (
        <LinkButton size="small" data-act="open" to="/doc/$id" params={{ id: x.id }}>
          Open
        </LinkButton>
      )
  }
}

// WordsDone asks for the owner's words before it closes: what came of a meeting (optional), or why
// an item is dropped (required).
function WordsDone({
  busy,
  done,
  onDone,
}: {
  busy: boolean
  done: DoneKind
  onDone: (text: string) => void
}) {
  const [at, setAt] = useState<HTMLElement | null>(null)
  const [text, setText] = useState('')
  const meeting = done === 'meeting'
  return (
    <>
      <Button size="small" data-act="done" disabled={busy} onClick={(e) => setAt(e.currentTarget)}>
        {doneLabel(done)}
      </Button>
      <Popover anchorEl={at} open={!!at} onClose={() => setAt(null)}>
        <Box sx={{ p: 2, width: 320 }} onKeyDown={(e) => e.stopPropagation()}>
          <TextField
            multiline
            minRows={meeting ? 3 : 2}
            fullWidth
            autoFocus
            size="small"
            label={meeting ? 'What came of it (optional)' : 'Why it needs nothing (required)'}
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
          <Button
            size="small"
            variant="contained"
            sx={{ mt: 1 }}
            data-act={meeting ? 'held' : 'dropped'}
            disabled={!meeting && !text.trim()}
            onClick={() => {
              const t = text.trim()
              setAt(null)
              setText('')
              onDone(t)
            }}
          >
            {meeting ? 'Held' : 'Drop'}
          </Button>
        </Box>
      </Popover>
    </>
  )
}

// useItemActions acts on one item: Done (close), Later (later), and Reopen (reopen), each with a
// toast whose Undo takes it back: the receipt of Done or Reopen is undone (receipt_revert), and an
// item put off is brought back (reopen). onGone says the item left the list or came back.
function useItemActions(
  x: Row,
  closed: boolean,
  deferred: boolean,
  onGone: (gone: boolean) => void,
) {
  const toasts = useToasts()
  const qc = useQueryClient()
  const [busy, setBusy] = useState(false)
  const name = x.title || x.id
  const acts = agendaActions(x.kind, closed, deferred)
  const fail = (verb: 'done' | 'later' | 'return', e: unknown) =>
    toasts.push({ text: failText(verb, [name], errorText(e)), tone: 'error' })

  const run = async (verb: 'done' | 'later' | 'return', call: () => Promise<void>) => {
    setBusy(true)
    try {
      await call()
      await qc.invalidateQueries()
    } catch (e) {
      fail(verb, e)
    } finally {
      setBusy(false)
    }
  }

  const undoReceipt = (receipt: string | null | undefined) =>
    receipt
      ? async () => {
          await revertReceipt(receipt)
          onGone(false)
          await qc.invalidateQueries()
        }
      : undefined

  const done = (text: string) =>
    run('done', async () => {
      // A meeting's notes go in its notes first, as the composer does; the close keeps them too.
      if (text && acts.done === 'meeting') {
        const before = await readNotes(x.id)
        await saveNotes(x.id, before.hash, notesWithEntry(before.md, localDate(), text, []))
      }
      const r = await closeAny(closeBody(acts.done, x.id, acts.done === 'meeting' ? '' : text))
      onGone(true)
      const undo = undoReceipt(r.receipt)
      toasts.push({
        text: toastText('done', [name]),
        undo,
        note: undo ? undefined : 'This close left no receipt to undo.',
      })
    })

  const putOff = (until: string) =>
    run('later', async () => {
      await laterAny({ id: x.id, until })
      onGone(true)
      toasts.push({
        text: toastText('later', [name]),
        undo: async () => {
          await reopenAny({ id: x.id })
          onGone(false)
          await qc.invalidateQueries()
        },
      })
    })

  // reopen opens a closed item, or brings back one put off.
  const reopen = () =>
    run('return', async () => {
      const r = await reopenAny({ id: x.id })
      onGone(true)
      toasts.push({
        text: closed ? `Reopened: ${name}` : toastText('return', [name]),
        undo: undoReceipt(r.receipt),
      })
    })

  return { acts, busy, done, putOff, reopen }
}

// ItemActions are the Done, Later, and Reopen buttons of one item, for a session that may decide.
// Reopen shows on a closed item; Done and Later on an open one, as its kind allows.
export function ItemActions({
  x,
  closed = false,
  deferred = false,
  onGone = () => {},
  open = true,
  children,
}: {
  x: Row
  closed?: boolean
  deferred?: boolean
  onGone?: (gone: boolean) => void
  open?: boolean
  children?: ReactNode
}) {
  const { session } = useRouteContext({ from: '/_authed' })
  const { acts, busy, done, putOff, reopen } = useItemActions(x, closed, deferred, onGone)
  const decide = canDecide(session)
  return (
    <Stack
      direction="row"
      spacing={0.5}
      sx={{ alignItems: 'center', flexShrink: 0 }}
      data-item-actions={x.id}
    >
      {decide && (acts.done === 'meeting' || acts.done === 'drop') && (
        <WordsDone busy={busy} done={acts.done} onDone={(t) => void done(t)} />
      )}
      {decide && (acts.done === 'task' || acts.done === 'ask' || acts.done === 'resolve') && (
        <Button size="small" data-act="done" disabled={busy} onClick={() => void done('')}>
          {doneLabel(acts.done)}
        </Button>
      )}
      {decide && acts.later && <LaterButton disabled={busy} onLater={(u) => void putOff(u)} />}
      {decide && acts.reopen && (
        <Button size="small" data-act="reopen" disabled={busy} onClick={() => void reopen()}>
          {closed ? 'Reopen' : 'Bring back'}
        </Button>
      )}
      {children}
      {open && <OpenButton x={x} />}
    </Stack>
  )
}

// AgendaRow is one row of the agenda with the actions that fit its kind: Done, Later, and Open. The
// row collapses out when it is done or put off, and comes back on Undo or when the list reloads. A
// meeting row has Brief; needsYou opens the brief of a meeting row without a click.
export function AgendaRow({
  x,
  needsYou = false,
  children,
}: {
  x: Row
  needsYou?: boolean
  children: (actions: ReactNode) => ReactNode
}) {
  const [gone, setGone] = useState(false)
  const [how, setHow] = useState(false)
  const [brief, setBrief] = useState(() => briefOpensItself(x.kind, needsYou))
  const acts = agendaActions(x.kind)
  const actions = (
    <ItemActions x={x} onGone={setGone}>
      {acts.done === 'task' && x.kind === 'task' && (
        <Button size="small" data-act="how" aria-expanded={how} onClick={() => setHow((v) => !v)}>
          How
        </Button>
      )}
      {x.kind === 'meeting' && (
        <Button
          size="small"
          data-act="brief"
          aria-expanded={brief}
          onClick={() => setBrief((v) => !v)}
        >
          Brief
        </Button>
      )}
    </ItemActions>
  )
  return (
    <Collapse
      in={!gone}
      appear={false}
      unmountOnExit
      timeout={prefersReducedMotion() ? 0 : 200}
      data-agenda-row={x.id}
    >
      {children(actions)}
      {how && <DoItTask id={x.id} />}
      {brief && x.kind === 'meeting' && (
        <Box sx={{ pl: 2, pb: 1, borderLeft: 2, borderColor: 'divider' }}>
          <BriefBody id={x.id} />
        </Box>
      )}
    </Collapse>
  )
}
