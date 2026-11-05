import { useState, type ReactNode } from 'react'
import { useRouteContext } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Collapse from '@mui/material/Collapse'
import Popover from '@mui/material/Popover'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import { closeItem } from '@/api/composer'
import { readNotes, saveNotes } from '@/api/notes'
import { localDate } from '@/api/today'
import { stackReturn, useStackLater } from '@/api/proposals'
import { canDecide } from '@/client/session'
import { errorText } from '@/client/gqlClient'
import { DoItTask } from '@/components/DoIt'
import { LinkButton } from '@/components/links'
import { LaterButton } from '@/components/stack/LaterButton'
import { useToasts } from '@/components/ToastStack'
import { agendaActions, openTarget } from '@/lib/agenda'
import { notesWithEntry } from '@/lib/composer'
import { prefersReducedMotion } from '@/lib/toasts'
import { failText, toastText, whyNoUndo } from '@/lib/stackToast'

type Row = { id: string; title: string; kind?: string | null }

function OpenButton({ x }: { x: Row }) {
  switch (openTarget(x.kind)) {
    case 'incident':
      return (
        <LinkButton size="small" to="/incidents/$id" params={{ id: x.id }}>
          Open
        </LinkButton>
      )
    case 'rfx':
      return (
        <LinkButton size="small" to="/rfx/$id" params={{ id: x.id }}>
          Open
        </LinkButton>
      )
    case 'io':
      return (
        <LinkButton size="small" to="/io/$id" params={{ id: x.id }}>
          Open
        </LinkButton>
      )
    default:
      return (
        <LinkButton size="small" to="/doc/$id" params={{ id: x.id }}>
          Open
        </LinkButton>
      )
  }
}

// MeetingDone asks for what came of a meeting, optional, then holds it.
function MeetingDone({ busy, onHeld }: { busy: boolean; onHeld: (notes: string) => void }) {
  const [at, setAt] = useState<HTMLElement | null>(null)
  const [text, setText] = useState('')
  return (
    <>
      <Button size="small" data-act="done" disabled={busy} onClick={(e) => setAt(e.currentTarget)}>
        Done
      </Button>
      <Popover anchorEl={at} open={!!at} onClose={() => setAt(null)}>
        <Box sx={{ p: 2, width: 320 }} onKeyDown={(e) => e.stopPropagation()}>
          <TextField
            multiline
            minRows={3}
            fullWidth
            autoFocus
            size="small"
            label="What came of it (optional)"
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
          <Button
            size="small"
            variant="contained"
            sx={{ mt: 1 }}
            data-act="held"
            onClick={() => {
              const t = text.trim()
              setAt(null)
              setText('')
              onHeld(t)
            }}
          >
            Held
          </Button>
        </Box>
      </Popover>
    </>
  )
}

// AgendaRow is one row of the agenda with the actions that fit its kind: Done, Later, and Open. The
// row collapses out when it is done or put off, and comes back on Undo (Later) or when the list
// reloads. Done by the owner cannot be undone here, as on the stack.
export function AgendaRow({
  x,
  children,
}: {
  x: Row
  children: (actions: ReactNode) => ReactNode
}) {
  const { session } = useRouteContext({ from: '/_authed' })
  const toasts = useToasts()
  const qc = useQueryClient()
  const later = useStackLater()
  const [gone, setGone] = useState(false)
  const [busy, setBusy] = useState(false)
  const [how, setHow] = useState(false)
  const name = x.title || x.id
  const acts = agendaActions(x.kind)
  const decide = canDecide(session)

  const fail = (verb: 'done' | 'later', e: unknown) =>
    toasts.push({ text: failText(verb, [name], errorText(e)), tone: 'error' })

  const done = async (notes: string) => {
    setBusy(true)
    try {
      if (notes) {
        const before = await readNotes(x.id)
        await saveNotes(x.id, before.hash, notesWithEntry(before.md, localDate(), notes, []))
      }
      await closeItem(x.id)
      setGone(true)
      toasts.push({ text: toastText('done', [name]), note: whyNoUndo('done') })
      await qc.invalidateQueries()
    } catch (e) {
      fail('done', e)
    } finally {
      setBusy(false)
    }
  }

  const putOff = async (until: string) => {
    setBusy(true)
    try {
      await later.mutateAsync({ questions: [x.id], until })
      setGone(true)
      toasts.push({
        text: toastText('later', [name]),
        undo: async () => {
          await stackReturn({ questions: [x.id] })
          setGone(false)
          await qc.invalidateQueries()
        },
      })
    } catch (e) {
      fail('later', e)
    } finally {
      setBusy(false)
    }
  }

  const actions = (
    <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center', flexShrink: 0 }}>
      {decide && acts.done === 'meeting' && (
        <MeetingDone busy={busy} onHeld={(n) => void done(n)} />
      )}
      {decide && (acts.done === 'task' || acts.done === 'ask') && (
        <Button size="small" data-act="done" disabled={busy} onClick={() => void done('')}>
          Done
        </Button>
      )}
      {decide && acts.later && <LaterButton disabled={busy} onLater={(u) => void putOff(u)} />}
      {acts.done === 'task' && (
        <Button size="small" data-act="how" aria-expanded={how} onClick={() => setHow((v) => !v)}>
          How
        </Button>
      )}
      <OpenButton x={x} />
    </Stack>
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
    </Collapse>
  )
}
