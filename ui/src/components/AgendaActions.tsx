import { useEffect, useState, type ReactNode } from 'react'
import { useRouteContext } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Collapse from '@mui/material/Collapse'
import Popover from '@mui/material/Popover'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import { doneWithNote, investigateWithNote } from '@/api/noted'
import { revertReceipt } from '@/api/receipts'
import { useLooking } from '@/api/requests'
import { laterAny, reopenAny } from '@/api/verbs'
import { canDecide } from '@/client/session'
import { errorText } from '@/client/gqlClient'
import { AnswerButton } from '@/components/AskActions'
import { DoItTask } from '@/components/DoIt'
import { DraftHint, NoteIcon } from '@/components/DraftHint'
import { useItemDraft } from '@/components/ItemDraft'
import { LinkButton } from '@/components/links'
import { LaterButton } from '@/components/stack/LaterButton'
import { useToasts } from '@/components/ToastStack'
import { useThemeReview } from '@/components/ThemeReview'
import { reviewsOnClose } from '@/lib/theme'
import { BriefBody } from '@/components/MeetingBrief'
import { draftHints } from '@/lib/composer'
import { agendaActions, doneLabel, investigable, openTarget, type DoneKind } from '@/lib/agenda'
import { briefOpensItself } from '@/lib/brief'
import { prefersReducedMotion } from '@/lib/toasts'
import { PinButton } from '@/components/PinButton'
import { isPoolRow } from '@/lib/pools'
import { failText, toastText } from '@/lib/stackToast'

type Row = { id: string; title: string; kind?: string | null }

function OpenButton({ x }: { x: Row }) {
  const t = openTarget(x.kind)
  const to = t === 'rfx' ? '/rfx/$id' : t === 'io' ? '/io/$id' : '/docs/$id'
  return (
    <LinkButton size="small" data-act="open" to={to} params={{ id: x.id }}>
      Open
    </LinkButton>
  )
}

// WordsDone asks for the owner's words before it closes: what came of a meeting (optional), or why
// an item is dropped (required).
function WordsDone({
  busy,
  done,
  draft = '',
  onDone,
}: {
  busy: boolean
  done: DoneKind
  // draft is what the owner typed in the composer: the words start from it.
  draft?: string
  onDone: (text: string) => void
}) {
  const hint = draft.trim() ? 'Starts from what you typed' : null
  const [at, setAt] = useState<HTMLElement | null>(null)
  const [text, setText] = useState('')
  const meeting = done === 'meeting'
  // While the popover is open the item's composer hides (ItemComposer): the popover sits on top
  // of it, and its outline would show behind the popover's.
  useEffect(() => {
    if (!at) return
    document.body.dataset.composerCovered = 'yes'
    return () => {
      delete document.body.dataset.composerCovered
    }
  }, [at])
  return (
    <>
      <DraftHint hint={hint}>
        <Button
          size="small"
          data-act="done"
          data-uses-draft={hint ? 'yes' : undefined}
          disabled={busy}
          startIcon={hint ? <NoteIcon /> : undefined}
          onClick={(e) => {
            if (draft.trim()) setText(draft.trim())
            setAt(e.currentTarget)
          }}
        >
          {doneLabel(done)}
        </Button>
      </DraftHint>
      <Popover anchorEl={at} open={!!at} onClose={() => setAt(null)}>
        <Box
          sx={{
            p: { xs: 1.5, sm: 2 },
            width: { xs: 'calc(100vw - 32px)', sm: 320 },
            maxWidth: 320,
          }}
          onKeyDown={(e) => e.stopPropagation()}
        >
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
          <Button size="small" sx={{ mt: 1, mr: 1 }} onClick={() => setAt(null)}>
            Back
          </Button>
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
            {meeting ? 'Mark done' : 'Drop'}
          </Button>
        </Box>
      </Popover>
    </>
  )
}

// useItemActions acts on one item: Done (close), Investigate (investigate), Later (later), and
// Reopen (reopen), each with a toast whose Undo takes it back: the receipt of Done or Reopen is
// undone (receipt_revert), the agent work of Investigate is cancelled, and an item put off is
// brought back (reopen). Done and Investigate first add what the owner typed in the item's composer
// to its notes; their Undo takes that entry out too. onGone says the item left the list or came
// back.
type Verb = 'done' | 'later' | 'return' | 'investigate'

export function useItemActions(
  x: Row,
  closed: boolean,
  deferred: boolean,
  onGone: (gone: boolean) => void,
) {
  const toasts = useToasts()
  const qc = useQueryClient()
  const draft = useItemDraft(x.id)
  const review = useThemeReview()
  const [busy, setBusy] = useState(false)
  const name = x.title || x.id
  const acts = agendaActions(x.kind, closed, deferred)
  const fail = (verb: Verb, e: unknown) =>
    toasts.push({
      text:
        verb === 'investigate'
          ? `Could not investigate ${name}: ${errorText(e)}`
          : failText(verb, [name], errorText(e)),
      tone: 'error',
      target: { id: x.id, kind: openTarget(x.kind) },
    })

  // run says whether the call went through; a failure is a toast.
  const run = async (verb: Verb, call: () => Promise<void>): Promise<boolean> => {
    setBusy(true)
    try {
      await call()
      await qc.invalidateQueries()
      return true
    } catch (e) {
      fail(verb, e)
      return false
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

  // done closes the item. words are the note: a meeting's notes or a drop's reason from the Mark
  // done or Drop box, or the composer's words; with none, the composer's draft. Files go with the note.
  const done = (words?: string, files?: File[]) =>
    run('done', async () => {
      const text = words ?? draft?.text.trim() ?? ''
      // Closing a theme that open items hang on opens the review instead: the owner decides on
      // each, and Confirm closes the theme with them as one batch.
      const outcome = acts.done === 'drop' ? 'dropped' : undefined
      if (
        reviewsOnClose(x.kind, outcome) &&
        (await review({
          theme: x.id,
          title: name,
          end: { kind: 'close', outcome, reason: text || undefined },
          onDone: () => {
            draft?.clear()
            onGone(true)
          },
        }))
      )
        return
      const r = await doneWithNote({
        id: x.id,
        done: acts.done,
        text,
        files: files ?? draft?.files ?? [],
        // A drop keeps its reason on the close too; anything else keeps the words in the notes.
        reason: acts.done === 'drop' ? text : undefined,
      })
      draft?.clear()
      onGone(true)
      const undo = r.undo
        ? async () => {
            await r.undo?.()
            onGone(false)
            await qc.invalidateQueries()
          }
        : undefined
      toasts.push({
        text: toastText('done', [name]),
        target: { id: x.id, kind: openTarget(x.kind) },
        undo,
        note: !undo
          ? 'This close left no log entry to undo.'
          : r.noted
            ? 'Undo opens it again and takes the note out.'
            : undefined,
      })
    })

  // look sends the item to investigate, with the composer's words as the note and the hint.
  const look = (words?: string, files?: File[]) =>
    run('investigate', async () => {
      const r = await investigateWithNote({
        id: x.id,
        text: words ?? draft?.text.trim() ?? '',
        files: files ?? draft?.files ?? [],
      })
      draft?.clear()
      toasts.push({
        target: { id: x.id, kind: openTarget(x.kind) },
        text: r.appended
          ? `Added to the open investigation: ${name}`
          : `Agent investigating: ${name}`,
        undo: r.undo
          ? async () => {
              await r.undo?.()
              await qc.invalidateQueries()
            }
          : undefined,
        note: r.appended
          ? 'The item already had open agent work; the note went into it.'
          : r.noted
            ? 'Undo cancels the agent work and takes the note out.'
            : undefined,
      })
    })

  // putOff and reopen take the composer's words as the reason, as Done and Investigate take them as
  // the note. Files stay in the box: a reason holds no files.
  const reason = () => draft?.text.trim() || undefined
  const putOff = (until: string) =>
    run('later', async () => {
      await laterAny({ id: x.id, until, reason: reason() })
      draft?.setText('')
      onGone(true)
      toasts.push({
        text: toastText('later', [name]),
        target: { id: x.id, kind: openTarget(x.kind) },
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
      const r = await reopenAny({ id: x.id, reason: reason() })
      draft?.setText('')
      onGone(true)
      toasts.push({
        text: closed ? `Reopened: ${name}` : toastText('return', [name]),
        target: { id: x.id, kind: openTarget(x.kind) },
        undo: undoReceipt(r.receipt),
      })
    })

  return { acts, busy, done, look, putOff, reopen, draft }
}

// ItemActions are the Done, Investigate, Later, and Reopen buttons of one item, for a session that
// may decide. Reopen shows on a closed item; Done, Investigate, and Later on an open one, as its
// kind allows. Near a composer, Done and Investigate take what the owner typed there as a note,
// Later and Reopen as the reason; while the box has words, each of them shows a note icon and says
// so on hover (DraftHint). The row wraps, so it never pushes the page wider than the screen.
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
  const { acts, busy, done, look, putOff, reopen, draft } = useItemActions(
    x,
    closed,
    deferred,
    onGone,
  )
  const looking = useLooking()
  const decide = canDecide(session)
  // Investigate shows on an open item the op takes. While agent work is open on it, the item shows
  // its Agent looking chip, and Investigate comes back only to add a note the owner typed.
  const canLook =
    decide && !closed && investigable(x.kind) && (!looking.has(x.id) || !!draft?.text.trim())
  const hints = draftHints(draft?.text ?? '', draft?.files.length ?? 0)
  return (
    <Stack
      direction="row"
      useFlexGap
      // On a phone the row may shrink and wrap; from 600 px it keeps its buttons on one line.
      sx={{
        alignItems: 'center',
        flexWrap: 'wrap',
        gap: 0.5,
        minWidth: 0,
        flexShrink: { xs: 1, sm: 0 },
      }}
      data-item-actions={x.id}
    >
      {decide && (acts.done === 'meeting' || acts.done === 'drop') && (
        <WordsDone busy={busy} done={acts.done} draft={draft?.text} onDone={(t) => void done(t)} />
      )}
      {decide && acts.done === 'ask' && (
        <AnswerButton id={x.id} title={x.title} label="Done" fallback={() => void done()} />
      )}
      {decide && (acts.done === 'task' || acts.done === 'resolve') && (
        <DraftHint hint={hints.note}>
          <Button
            size="small"
            data-act="done"
            data-uses-draft={hints.note ? 'yes' : undefined}
            disabled={busy}
            startIcon={hints.note ? <NoteIcon /> : undefined}
            onClick={() => void done()}
          >
            {doneLabel(acts.done)}
          </Button>
        </DraftHint>
      )}
      {canLook && (
        <DraftHint hint={hints.look}>
          <Button
            size="small"
            data-act="investigate"
            data-uses-draft={hints.look ? 'yes' : undefined}
            disabled={busy}
            startIcon={hints.look ? <NoteIcon /> : undefined}
            onClick={() => void look()}
          >
            Investigate
          </Button>
        </DraftHint>
      )}
      {decide && acts.later && (
        <LaterButton
          disabled={busy}
          onLater={(u) => void putOff(u)}
          startIcon={hints.reason ? <NoteIcon /> : undefined}
          data-uses-draft={hints.reason ? 'yes' : undefined}
          wrap={(b) => <DraftHint hint={hints.reason}>{b}</DraftHint>}
        />
      )}
      {decide && acts.reopen && (
        <DraftHint hint={hints.reason}>
          <Button
            size="small"
            data-act="reopen"
            data-uses-draft={hints.reason ? 'yes' : undefined}
            disabled={busy}
            startIcon={hints.reason ? <NoteIcon /> : undefined}
            onClick={() => void reopen()}
          >
            {closed ? 'Reopen' : 'Bring back'}
          </Button>
        </DraftHint>
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
    <>
      {!isPoolRow(x) && <PinButton id={x.id} />}
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
    </>
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
