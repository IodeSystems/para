import { useState } from 'react'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import Link from '@mui/material/Link'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import Alert from '@mui/material/Alert'
import {
  useOutcomes,
  useReceipt,
  useReceiptCheck,
  useReceiptRevert,
  useReceipts,
  type Receipt,
} from '@/api/receipts'
import { errorText } from '@/client/gqlClient'
import { Empty, Section, StatusChip } from '@/components/Bits'
import { CopyButton } from '@/components/CopyButton'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { LinkButton } from '@/components/links'
import { DoItTask } from '@/components/DoIt'
import { useToasts } from '@/components/ToastStack'
import { useRouteContext, useRouter } from '@tanstack/react-router'
import { canDecide } from '@/client/session'
import { diffKind } from '@/lib/diff'
import { checkTone, newestFirst, noteRequired, receiptTime, undoCommand } from '@/lib/receipts'
import { refKind, refLinks } from '@/lib/refs'
import { exactTime, humanTime } from '@/lib/when'

const mono = { fontFamily: 'ui-monospace, monospace', fontSize: 13 } as const

// DiffLines shows the before and after of one write. It loads only when opened.
function DiffLines({ id }: { id: string }) {
  const q = useReceipt(id, true)
  return (
    <DataState q={q}>
      {(d) => (
        <Box
          component="pre"
          aria-label="What this write changed"
          sx={{ ...mono, m: 0, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}
        >
          {d.lines.length === 0 && 'No line changed.'}
          {d.lines.map((l, n) => {
            const k = diffKind(l)
            return (
              <Box
                component="span"
                key={n}
                sx={{
                  display: 'block',
                  color:
                    k === 'add' ? 'success.main' : k === 'del' ? 'error.main' : 'text.secondary',
                }}
              >
                {l}
              </Box>
            )
          })}
        </Box>
      )}
    </DataState>
  )
}

// ReceiptCard shows one agent write: when, who, what, to which document, why, and from what.
// Undo is a command to copy: the viewer never writes.
// withActions adds Open, Undo, and for a task the Do it block; the section on a document's own page
// leaves them off, since the document is already open.
export function ReceiptCard({
  r,
  showActor = true,
  withActions = true,
}: {
  r: Receipt
  showActor?: boolean
  withActions?: boolean
}) {
  const t = receiptTime(r)
  const cmd = undoCommand(r.id)
  // The diff loads when a person opens it, not for every row.
  const [open, setOpen] = useState(false)
  return (
    <Box data-receipt={r.id} sx={{ width: '100%' }}>
      <Stack
        direction="row"
        spacing={0.75}
        useFlexGap
        sx={{ alignItems: 'center', flexWrap: 'wrap' }}
      >
        <Tooltip title={exactTime(t)}>
          <Typography component="time" dateTime={t} variant="body2" color="text.secondary">
            {humanTime(t)}
          </Typography>
        </Tooltip>
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          {showActor && <>{r.actor}: </>}
          {r.op.replace(/_/g, ' ')}{' '}
          {r.doc === 'vault' ? (
            'the vault'
          ) : (
            <DocLink id={r.doc}>{r.docName || r.title || r.doc}</DocLink>
          )}
        </Typography>
        {r.selfReported && <StatusChip tone="wait" label="self-reported" />}
        {r.background && <StatusChip tone="quiet" label="background" />}
      </Stack>
      {r.ownerSaid && (
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          Owner said: &ldquo;{r.ownerSaid}&rdquo;
        </Typography>
      )}
      {r.why && (
        <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
          Why: {r.why}
        </Typography>
      )}
      <SourceLinks r={r} />
      {r.verify && (
        <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
          Verify: {r.verify}
        </Typography>
      )}
      {withActions && r.doc !== 'vault' && r.op.startsWith('task_') && <DoItTask id={r.doc} />}
      {withActions && <ReceiptActions r={r} />}
      <Box
        component="details"
        onToggle={(e) => setOpen(e.currentTarget.open)}
        sx={{ mt: 0.5, fontSize: '0.8125rem' }}
      >
        <Box component="summary" sx={{ cursor: 'pointer', width: 'fit-content' }}>
          What changed
        </Box>
        {open && <DiffLines id={r.id} />}
      </Box>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', mt: 0.5 }}>
        <Typography
          component="code"
          aria-label="Undo command"
          sx={{ ...mono, overflowWrap: 'anywhere', flexGrow: 1 }}
        >
          {cmd}
        </Typography>
        <CopyButton getText={() => cmd} label="Copy undo command" />
      </Stack>
    </Box>
  )
}

// ReceiptSection lists the agent writes to one document, newest first.
export function ReceiptSection({ title, doc }: { title: string; doc: string }) {
  const q = useReceipts({ doc, limit: 30 })
  const items = newestFirst(q.data ?? [])
  return (
    <Section title={title} count={items.length} hideEmpty>
      <List dense disablePadding>
        {items.map((r) => (
          <ListItem key={r.id} disableGutters divider sx={{ display: 'block' }}>
            <ReceiptCard r={r} withActions={false} />
          </ListItem>
        ))}
      </List>
    </Section>
  )
}

// SourceLinks shows the sources of a write, resolved by the server and without repeats: a vault
// document opens in the app, a web address or a source reference with a link template opens in a
// new tab, and the rest is plain text (a source with no link says its title).
function SourceLinks({ r }: { r: Pick<Receipt, 'sources' | 'sourceLinks'> }) {
  const router = useRouter()
  const links = refLinks(r)
  if (links.length === 0) return null
  return (
    <Typography
      variant="body2"
      color="text.secondary"
      component="div"
      data-sources
      sx={{ overflowWrap: 'anywhere' }}
    >
      Sources:{' '}
      {links.map((l, i) => {
        const kind = refKind(l)
        return (
          <span key={`${l.text}-${i}`}>
            {i > 0 && ', '}
            {kind === 'web' ? (
              <Link
                href={l.url ?? undefined}
                target="_blank"
                rel="noopener noreferrer"
                underline="hover"
              >
                {l.text}
              </Link>
            ) : kind === 'app' ? (
              <Link
                href={l.url ?? undefined}
                underline="hover"
                onClick={(e) => {
                  if (e.metaKey || e.ctrlKey || e.shiftKey || !l.url) return
                  e.preventDefault()
                  router.history.push(l.url)
                }}
              >
                {l.text}
              </Link>
            ) : (
              l.text
            )}
          </span>
        )
      })}
    </Typography>
  )
}

// ReceiptActions: Open the document, and Undo the write (a toast says what happened). Undo is
// disabled with the reason once the write is undone; the server refuses it, with the reason, when a
// later write changed the document.
function ReceiptActions({ r }: { r: Receipt }) {
  const { session } = useRouteContext({ from: '/_authed' })
  const revert = useReceiptRevert()
  const toasts = useToasts()
  const undone = !!r.undone
  const run = () =>
    revert.mutate(r.id, {
      onSuccess: () =>
        toasts.push({ text: `Undid ${r.op.replace(/_/g, ' ')}: ${r.docName || r.title || r.doc}` }),
      onError: (e) => toasts.push({ text: `Could not undo: ${errorText(e)}`, tone: 'error' }),
    })
  return (
    <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap', mt: 0.5 }}>
      {r.doc !== 'vault' && (
        <LinkButton size="small" variant="outlined" to="/doc/$id" params={{ id: r.doc }}>
          Open
        </LinkButton>
      )}
      {canDecide(session) && (
        <Tooltip title={undone ? 'Already undone.' : ''}>
          <span>
            <Button
              size="small"
              color="warning"
              disabled={undone || revert.isPending}
              onClick={run}
              aria-label={undone ? 'Undo (already undone)' : 'Undo this write'}
            >
              Undo
            </Button>
          </span>
        </Tooltip>
      )}
    </Stack>
  )
}

// ReceiptRow is one agent write on the Receipts page: what it did, why, from what, how to
// check it, and the owner's actions. Undo and "wrong" ask in the row, never in a browser dialog.
export function ReceiptRow({ r, undone }: { r: Receipt; undone: boolean }) {
  const t = receiptTime(r)
  const revert = useReceiptRevert()
  const check = useReceiptCheck()
  const [asking, setAsking] = useState<'undo' | 'wrong' | null>(null)
  const [note, setNote] = useState('')
  const [open, setOpen] = useState(false)
  const busy = revert.isPending || check.isPending
  const err = revert.error ?? check.error
  const cancel = () => {
    setAsking(null)
    setNote('')
  }
  return (
    <Box data-receipt={r.id} data-check={r.check} data-undone={undone} sx={{ width: '100%' }}>
      <Stack
        direction="row"
        spacing={0.75}
        useFlexGap
        sx={{ alignItems: 'center', flexWrap: 'wrap' }}
      >
        <Tooltip title={exactTime(t)}>
          <Typography component="time" dateTime={t} variant="body2" color="text.secondary">
            {humanTime(t)}
          </Typography>
        </Tooltip>
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          {r.actor}: {r.op.replace(/_/g, ' ')}{' '}
          {r.doc === 'vault' ? (
            'the vault'
          ) : (
            <DocLink id={r.doc}>{r.docName || r.title || r.doc}</DocLink>
          )}
        </Typography>
        <span data-check-chip={r.check}>
          <StatusChip tone={checkTone(r.check)} label={r.check} />
        </span>
        {undone && <StatusChip tone="attn" label="undone" />}
        {r.confidence && <StatusChip tone="quiet" label={`${r.confidence} confidence`} />}
        {r.selfReported && <StatusChip tone="wait" label="self-reported" />}
      </Stack>
      {(r.by || r.onBehalfOf) && (
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          {[r.by && `Done by ${r.by}`, r.onBehalfOf === 'owner' && 'as the owner']
            .filter(Boolean)
            .join(', ')}
        </Typography>
      )}
      {r.ownerSaid && (
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          Owner said: &ldquo;{r.ownerSaid}&rdquo;
        </Typography>
      )}
      {r.why && (
        <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
          Why: {r.why}
        </Typography>
      )}
      <SourceLinks r={r} />
      {r.verify && (
        <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
          Verify: {r.verify}
        </Typography>
      )}
      {r.check !== 'unchecked' && (
        <Typography
          variant="body2"
          color="text.secondary"
          data-check-note
          sx={{ overflowWrap: 'anywhere' }}
        >
          {r.check === 'verified' ? 'Verified' : 'Marked wrong'}
          {r.checkedBy ? ` by ${r.checkedBy}` : ''}
          {r.checkNote ? `: ${r.checkNote}` : '.'}
        </Typography>
      )}
      <Box
        component="details"
        onToggle={(e) => setOpen(e.currentTarget.open)}
        sx={{ mt: 0.5, fontSize: '0.8125rem' }}
      >
        <Box component="summary" sx={{ cursor: 'pointer', width: 'fit-content' }}>
          What changed
        </Box>
        {open && <DiffLines id={r.id} />}
      </Box>
      <Stack
        direction="row"
        spacing={1}
        useFlexGap
        sx={{ alignItems: 'center', flexWrap: 'wrap', mt: 0.5 }}
      >
        {asking === 'wrong' ? (
          <>
            <TextField
              size="small"
              label="What is wrong"
              value={note}
              onChange={(e) => setNote(e.target.value)}
              required
              sx={{ flex: '1 1 220px' }}
            />
            <Button
              size="small"
              color="error"
              disabled={busy || noteRequired('wrong', note)}
              onClick={() =>
                check.mutate({ id: r.id, verdict: 'wrong', note }, { onSuccess: cancel })
              }
            >
              Save wrong
            </Button>
            <Button size="small" disabled={busy} onClick={cancel}>
              Cancel
            </Button>
          </>
        ) : asking === 'undo' ? (
          <>
            <Typography variant="body2">Put the document back to before this write?</Typography>
            <Button
              size="small"
              color="error"
              variant="contained"
              disabled={busy}
              onClick={() => revert.mutate(r.id, { onSuccess: cancel })}
            >
              Confirm undo
            </Button>
            <Button size="small" disabled={busy} onClick={cancel}>
              Cancel
            </Button>
          </>
        ) : (
          <>
            {!undone && (
              <Button
                size="small"
                disabled={busy}
                onClick={() => {
                  revert.reset()
                  setAsking('undo')
                }}
              >
                Undo
              </Button>
            )}
            {r.check !== 'verified' && (
              <Button
                size="small"
                disabled={busy}
                onClick={() => check.mutate({ id: r.id, verdict: 'verified' })}
              >
                Mark verified
              </Button>
            )}
            {r.check !== 'wrong' && (
              <Button
                size="small"
                disabled={busy}
                onClick={() => {
                  check.reset()
                  setAsking('wrong')
                }}
              >
                Mark wrong
              </Button>
            )}
          </>
        )}
      </Stack>
      {err && (
        <Alert severity="error" sx={{ mt: 0.5 }} data-receipt-error>
          {errorText(err)}
        </Alert>
      )}
    </Box>
  )
}

const OUTCOME_HINT = 'Closes and undos in the window. Empty dates mean the last 30 days.'

// OutcomesSection reports how work ended: task closes by outcome and by actor, and writes undone.
export function OutcomesSection({ since, until }: { since?: string; until?: string }) {
  const q = useOutcomes({ since, until })
  return (
    <DataState q={q}>
      {(o) => (
        <Section title="Outcomes" count={o.closes}>
          <Box data-outcomes>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
              {o.since} to {o.until}. {OUTCOME_HINT}
            </Typography>
            <Typography variant="subtitle2" component="h3">
              Task closes by outcome
            </Typography>
            {o.outcomes.length === 0 ? (
              <Empty>No task closed in this window.</Empty>
            ) : (
              <Stack direction="row" spacing={0.75} useFlexGap sx={{ flexWrap: 'wrap', my: 0.5 }}>
                {o.outcomes.map((c) => (
                  <StatusChip key={c.key} tone="quiet" label={`${c.key} ${c.count}`} />
                ))}
              </Stack>
            )}
            {o.byActor.length > 0 && (
              <>
                <Typography variant="subtitle2" component="h3" sx={{ mt: 1 }}>
                  By actor
                </Typography>
                <List dense disablePadding>
                  {o.byActor.map((a) => (
                    <ListItem key={a.actor} disableGutters data-actor-outcomes={a.actor}>
                      <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
                        {a.actor}: {a.total} (
                        {a.outcomes.map((c) => `${c.key} ${c.count}`).join(', ')})
                      </Typography>
                    </ListItem>
                  ))}
                </List>
              </>
            )}
            <Typography variant="subtitle2" component="h3" sx={{ mt: 1 }}>
              Undone writes
            </Typography>
            {o.undos.length === 0 ? (
              <Empty>No write was undone.</Empty>
            ) : (
              <List dense disablePadding>
                {o.undos.map((u, i) => (
                  <ListItem key={`${u.receipt}-${i}`} disableGutters divider data-undo={u.receipt}>
                    <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
                      {humanTime(u.at)}: {u.by} undid {u.receipt} on <DocLink id={u.doc} />
                    </Typography>
                  </ListItem>
                ))}
              </List>
            )}
          </Box>
        </Section>
      )}
    </DataState>
  )
}
