import { useEffect, useRef, useState } from 'react'
import { useRouteContext } from '@tanstack/react-router'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import {
  useThreadEdit,
  useThreadOf,
  useThreadPost,
  useThreadWithdraw,
  withdrawTurn,
  type ThreadTurnData,
} from '@/api/thread'
import { errorText } from '@/client/gqlClient'
import { useQueryClient } from '@tanstack/react-query'
import { canDecide } from '@/client/session'
import { Section, StatusChip } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'
import { Markdown } from '@/components/Markdown'
import { useToasts } from '@/components/ToastStack'
import { exactTime, humanTime } from '@/lib/when'
import { composerKey, isMac, sendKeyHint } from '@/lib/composer'
import {
  EDIT_TURN,
  requestEditTurn,
  takePendingEdit,
  TURN_UNDO_MS,
  turnEditable,
  turnWithdrawable,
} from '@/lib/threadEdit'
import {
  ioTurnLabel,
  kindLabel,
  lastRequestTurn,
  orderTurns,
  statusText,
  threadChips,
} from '@/lib/thread'

// Thread is the panel of an item's thread: the owner's turns and the agent's answers, oldest first,
// the chips for what waits, and (composer) a box to post the next turn. The shell's live refresh
// keeps it current.

const when = (s: string) => humanTime(s)

export function Thread({ id, composer = false }: { id: string; composer?: boolean }) {
  const q = useThreadOf(id)
  const t = q.data
  if (!t) return null
  const turns = orderTurns(t.turns)
  const chips = threadChips(t)
  return (
    <Section title="Thread" count={turns.length}>
      <Box data-thread={id}>
        {chips.length > 0 && (
          <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', mb: 1 }}>
            {chips.map((c) => (
              <StatusChip key={c.label} tone={c.tone} label={c.label} />
            ))}
          </Stack>
        )}
        {turns.length === 0 && (
          <Typography variant="body2" color="text.secondary">
            No turns yet.
          </Typography>
        )}
        <Stack spacing={1.5}>
          {turns.map((x, i) => (
            <Turn key={`${x.request ?? x.io ?? ''}-${i}`} t={x} />
          ))}
        </Stack>
        {composer && <ThreadComposer id={id} />}
      </Box>
    </Section>
  )
}

function Turn({ t }: { t: ThreadTurnData }) {
  return t.kind === 'io' ? <IoTurn t={t} /> : <RequestTurn t={t} />
}

function RequestTurn({ t }: { t: ThreadTurnData }) {
  const { session } = useRouteContext({ from: '/_authed' })
  const decide = canDecide(session)
  const toasts = useToasts()
  const withdraw = useThreadWithdraw()
  // editing opens the words in place; the toast after a send asks for it with requestEditTurn.
  const [editing, setEditing] = useState(() => takePendingEdit(t.request))
  useEffect(() => {
    const on = () => {
      if (takePendingEdit(t.request)) setEditing(true)
    }
    window.addEventListener(EDIT_TURN, on)
    return () => window.removeEventListener(EDIT_TURN, on)
  }, [t.request])
  const mine = decide && !!t.request && !!t.turnKind && t.kind === 'request'
  const doWithdraw = async () => {
    if (!t.request) return
    try {
      const r = await withdraw.mutateAsync(t.request)
      toasts.push({
        text: 'Withdrawn',
        undo: r.undo
          ? async () => {
              await r.undo?.()
            }
          : undefined,
        note: r.undo ? undefined : 'This withdraw left no receipt to undo.',
      })
    } catch (e) {
      toasts.push({ text: `Could not withdraw: ${errorText(e)}`, tone: 'error' })
    }
  }
  const status = statusText(t, when)
  const failed = t.status === 'failed'
  const reply = t.reply || t.result
  return (
    <Box sx={{ pl: 1.5, borderLeft: 2, borderColor: 'divider' }} data-thread-turn={t.request}>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexWrap: 'wrap' }} useFlexGap>
        {t.turnKind && t.turnKind !== 'other' && <Chip size="small" label={kindLabel(t.turnKind)} />}
        <Typography variant="caption" color="text.secondary" title={exactTime(t.at)}>
          {when(t.at)}
        </Typography>
      </Stack>
      {t.quote && (
        <Box
          component="blockquote"
          sx={{
            m: 0,
            my: 0.5,
            pl: 1,
            borderLeft: 2,
            borderColor: 'divider',
            color: 'text.secondary',
          }}
        >
          <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
            {t.quote}
          </Typography>
        </Box>
      )}
      {editing && t.request ? (
        <TurnEditor t={t} onDone={() => setEditing(false)} />
      ) : (
        t.ownerWords && (
          <Typography
            variant="body2"
            sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', mt: 0.5 }}
          >
            {t.ownerWords}
          </Typography>
        )
      )}
      {(t.edited?.length ?? 0) > 0 && (
        <Box component="details" sx={{ mt: 0.5 }} data-thread-edited>
          <Box
            component="summary"
            sx={{ cursor: 'pointer', color: 'text.secondary', fontSize: '0.75rem' }}
          >
            Edited ({t.edited?.length})
          </Box>
          {t.edited?.map((e, i) => (
            <Typography
              key={i}
              variant="caption"
              color="text.secondary"
              sx={{ display: 'block', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}
              title={exactTime(e.heading)}
            >
              Was: {e.text}
            </Typography>
          ))}
        </Box>
      )}
      {mine && !editing && (turnEditable(t.status) || turnWithdrawable(t.status)) && (
        <Stack direction="row" spacing={0.5} sx={{ mt: 0.5 }} data-thread-turn-actions>
          {turnEditable(t.status) && (
            <Button size="small" data-act="edit-turn" onClick={() => setEditing(true)}>
              Edit
            </Button>
          )}
          <Button
            size="small"
            color="inherit"
            data-act="withdraw-turn"
            disabled={withdraw.isPending}
            onClick={() => void doWithdraw()}
          >
            Withdraw
          </Button>
        </Stack>
      )}
      <Typography
        variant="caption"
        sx={{ display: 'block', mt: 0.5 }}
        color={failed ? 'error' : 'text.secondary'}
      >
        {status}
        {t.request && (
          <>
            {' - '}
            <DocLink id={t.request}>{t.request}</DocLink>
          </>
        )}
      </Typography>
      {reply && (
        <Box sx={{ mt: 0.5 }} data-thread-reply>
          <Markdown source={reply} />
        </Box>
      )}
      {failed && t.error && (
        <Typography variant="body2" color="error" sx={{ overflowWrap: 'anywhere' }} role="alert">
          {t.error}
        </Typography>
      )}
      {(t.notes?.length ?? 0) > 0 && (
        <Box component="ul" sx={{ m: 0, mt: 0.5, pl: 2 }}>
          {t.notes?.map((n, i) => (
            <Typography component="li" variant="caption" color="text.secondary" key={i}>
              {n.actor}: {n.text}
            </Typography>
          ))}
        </Box>
      )}
      {t.receipts.length > 0 && (
        <Box sx={{ mt: 0.5 }}>
          <Typography variant="caption" sx={{ fontWeight: 600 }}>
            What it did
          </Typography>
          {t.receipts.map((r) => (
            <Typography
              key={r.id}
              variant="caption"
              sx={{
                display: 'block',
                overflowWrap: 'anywhere',
                textDecoration: r.undone ? 'line-through' : undefined,
              }}
            >
              {r.op} <DocLink id={r.doc}>{r.docName || r.doc}</DocLink>
              {r.why ? `: ${r.why}` : ''}
              {r.undone ? ' (undone)' : ''}
            </Typography>
          ))}
        </Box>
      )}
    </Box>
  )
}

// TurnEditor changes the words of a queued turn in place. Save calls thread_edit, which keeps the
// old words in the request; a turn an agent claimed meanwhile is refused with a plain message.
function TurnEditor({ t, onDone }: { t: ThreadTurnData; onDone: () => void }) {
  const edit = useThreadEdit()
  const toasts = useToasts()
  const [text, setText] = useState(t.ownerWords ?? '')
  const changed = text.trim() !== '' && text.trim() !== (t.ownerWords ?? '').trim()
  const save = async () => {
    if (!t.request || !changed) return
    try {
      await edit.mutateAsync({ id: t.request, text: text.trim() })
      toasts.push({ text: 'Turn changed', note: 'The old words stay in the thread under Edited.' })
      onDone()
    } catch (e) {
      toasts.push({ text: `Could not change the turn: ${errorText(e)}`, tone: 'error' })
    }
  }
  return (
    <Stack spacing={0.5} sx={{ mt: 0.5 }} data-thread-edit={t.request}>
      <TextField
        size="small"
        autoFocus
        multiline
        minRows={2}
        maxRows={10}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') return onDone()
          if (
            composerKey({
              key: e.key,
              ctrlKey: e.ctrlKey,
              metaKey: e.metaKey,
              isComposing: e.nativeEvent.isComposing,
            })
          ) {
            e.preventDefault()
            void save()
          }
        }}
        helperText={sendKeyHint(isMac())}
        slotProps={{ htmlInput: { 'aria-label': 'Edit the turn' } }}
      />
      <Stack direction="row" spacing={1}>
        <Button
          size="small"
          variant="contained"
          data-act="save-turn"
          disabled={!changed || edit.isPending}
          onClick={() => void save()}
        >
          Save
        </Button>
        <Button size="small" onClick={onDone}>
          Cancel
        </Button>
      </Stack>
    </Stack>
  )
}

function IoTurn({ t }: { t: ThreadTurnData }) {
  const dir = ioTurnLabel(t.direction, t.state)
  return (
    <Box sx={{ pl: 1.5, borderLeft: 2, borderColor: 'divider' }} data-thread-io={t.io}>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexWrap: 'wrap' }} useFlexGap>
        <Chip size="small" variant="outlined" label={`${dir}${t.medium ? ` (${t.medium})` : ''}`} />
        <Typography variant="caption" color="text.secondary" title={exactTime(t.at)}>
          {when(t.at)}
        </Typography>
        {t.io && <DocLink id={t.io}>{t.title || t.io}</DocLink>}
      </Stack>
      {t.body && (
        <Typography
          variant="body2"
          sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', mt: 0.5 }}
        >
          {t.body}
        </Typography>
      )}
      {(t.attempts ?? []).map((a, i) => (
        <Typography
          key={i}
          variant="caption"
          sx={{ display: 'block' }}
          color={a.result === 'failed' ? 'error' : 'text.secondary'}
        >
          {a.result} {when(a.at)}
          {a.tool ? ` via ${a.tool}` : ''}
          {a.error ? `: ${a.error}` : ''}
        </Typography>
      ))}
    </Box>
  )
}

// ThreadComposer posts one turn: the words and an optional quote (the selected text of
// the page, taken with "Quote selection").
function ThreadComposer({ id }: { id: string }) {
  const { session } = useRouteContext({ from: '/_authed' })
  const post = useThreadPost()
  const toasts = useToasts()
  const qc = useQueryClient()
  const [text, setText] = useState('')
  const [quote, setQuote] = useState('')
  const input = useRef<HTMLTextAreaElement>(null)
  if (!canDecide(session)) return null
  const send = async () => {
    try {
      const r = await post.mutateAsync({ id, text: text.trim(), quote: quote.trim() })
      toasts.push({
        text: 'Sent to the thread',
        note: r.note ?? undefined,
        target: { id },
        dwell: TURN_UNDO_MS,
        edit: () => requestEditTurn(r.request),
        undo: async () => {
          await withdrawTurn(r.request)
          await qc.invalidateQueries()
        },
      })
      setText('')
      setQuote('')
    } catch (e) {
      toasts.push({ text: `Could not post: ${errorText(e)}`, tone: 'error' })
    }
  }
  return (
    <Stack spacing={1} sx={{ mt: 2 }} data-thread-composer>
      {quote && (
        <TextField
          size="small"
          label="Quote"
          value={quote}
          onChange={(e) => setQuote(e.target.value)}
          multiline
          maxRows={4}
        />
      )}
      <TextField
        size="small"
        label="Tell the agent"
        inputRef={input}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          const k = composerKey({
            key: e.key,
            ctrlKey: e.ctrlKey,
            metaKey: e.metaKey,
            isComposing: e.nativeEvent.isComposing,
          })
          if (k && text.trim() && !post.isPending) {
            e.preventDefault()
            void send()
          }
        }}
        helperText={sendKeyHint(isMac())}
        multiline
        minRows={2}
        maxRows={10}
      />
      <Stack direction="row" spacing={1}>
        <Button
          size="small"
          variant="contained"
          disabled={!text.trim() || post.isPending}
          onClick={() => void send()}
        >
          Send
        </Button>
        <Button
          size="small"
          onClick={() => setQuote(window.getSelection()?.toString().trim() ?? '')}
        >
          Quote selection
        </Button>
      </Stack>
    </Stack>
  )
}

// ThreadSummary is the collapsed "Thread (n)" of a card: the last owner turn and its reply, so the
// owner sees why an item came back.
export function ThreadSummary({ id }: { id: string }) {
  const q = useThreadOf(id)
  const t = q.data
  if (!t || t.turns.length === 0) return null
  const last = lastRequestTurn(t.turns)
  const reply = last?.reply || last?.result
  return (
    <Box component="details" sx={{ mt: 1, fontSize: '0.8125rem' }} data-thread-summary={id}>
      <Box component="summary" sx={{ cursor: 'pointer', color: 'text.secondary' }}>
        Thread ({t.turns.length})
      </Box>
      {last && (
        <Box sx={{ mt: 0.5 }}>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
            {last.turnKind && last.turnKind !== 'other'
              ? `${kindLabel(last.turnKind)}: ${last.ownerWords}`
              : last.ownerWords}
          </Typography>
          <Typography
            variant="caption"
            color={last.status === 'failed' ? 'error' : 'text.secondary'}
          >
            {statusText(last, when)}
          </Typography>
          {reply && <Markdown source={reply} />}
        </Box>
      )}
    </Box>
  )
}
