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
import { TextLink } from '@/components/links'
import { diffKind } from '@/lib/diff'
import {
  checkTone,
  isUrl,
  newestFirst,
  noteRequired,
  receiptTime,
  undoCommand,
} from '@/lib/receipts'
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
export function ReceiptCard({ r, showActor = true }: { r: Receipt; showActor?: boolean }) {
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
      {(r.sources ?? []).length > 0 && (
        <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
          Sources: {r.sources?.join(', ')}
        </Typography>
      )}
      {r.verify && (
        <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
          Verify: {r.verify}
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
            <ReceiptCard r={r} />
          </ListItem>
        ))}
      </List>
    </Section>
  )
}

// SourceLinks shows the sources of a write: a vault id opens its document, an address opens
// the page it names.
function SourceLinks({ sources }: { sources: string[] }) {
  return (
    <Typography
      variant="body2"
      color="text.secondary"
      component="div"
      data-sources
      sx={{ overflowWrap: 'anywhere' }}
    >
      Sources:{' '}
      {sources.map((s, i) => (
        <span key={`${s}-${i}`}>
          {i > 0 && ', '}
          {isUrl(s) ? (
            <Link href={s} target="_blank" rel="noopener noreferrer" underline="hover">
              {s}
            </Link>
          ) : (
            <DocLink id={s} />
          )}
        </span>
      ))}
    </Typography>
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
      {(r.sources ?? []).length > 0 && <SourceLinks sources={r.sources ?? []} />}
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

const OUTCOME_HINT = 'Closes, denials, and undos in the window. Empty dates mean the last 30 days.'

// OutcomesSection reports how work ended: task closes by outcome and by actor, review items the
// owner denied with the reason, and writes undone.
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
              Denied review items
            </Typography>
            {o.denied.length === 0 ? (
              <Empty>No review item was denied.</Empty>
            ) : (
              <List dense disablePadding>
                {o.denied.map((d) => (
                  <ListItem
                    key={`${d.proposal}/${d.item}`}
                    disableGutters
                    divider
                    data-denied={`${d.proposal}/${d.item}`}
                    sx={{ display: 'block' }}
                  >
                    <TextLink to="/proposals/$id" params={{ id: d.proposal }} underline="hover">
                      {d.summary || d.title || d.item}
                    </TextLink>
                    <Typography
                      variant="body2"
                      color="text.secondary"
                      sx={{ overflowWrap: 'anywhere' }}
                    >
                      {d.actor}, {humanTime(d.at)}. Reason: {d.reason}
                    </Typography>
                  </ListItem>
                ))}
              </List>
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
