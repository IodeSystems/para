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
import Chip from '@mui/material/Chip'
import {
  useOutcomes,
  useReceipt,
  useReceiptCheck,
  useReceiptRevert,
  type Receipt,
} from '@/api/receipts'
import { errorText } from '@/client/gqlClient'
import { Empty, Section, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { useRouter } from '@tanstack/react-router'
import { diffKind } from '@/lib/diff'
import {
  actorClass,
  actorLabel,
  checkTone,
  noteRequired,
  opGroup,
  receiptTime,
} from '@/lib/receipts'
import { refKind, refLinks } from '@/lib/refs'
import { exactTime, humanTime } from '@/lib/when'

// Category colors, from the theme palette: one per actor class (filled chip) and one per op group
// (outlined chip). They name a kind, not a state, so they do not use the status tones.
const ACTOR_COLOR = { owner: 'primary', agent: 'info', para: 'default' } as const
const OP_COLOR = {
  add: 'success',
  edit: 'info',
  close: 'warning',
  send: 'primary',
  link: 'default',
  undo: 'error',
  system: 'default',
} as const

// ActorChip says who made a write; the raw actor is the hover text.
export function ActorChip({ actor }: { actor: string }) {
  return (
    <Chip
      size="small"
      data-actor-chip={actorClass(actor)}
      title={actor}
      label={actorLabel(actor)}
      color={ACTOR_COLOR[actorClass(actor)]}
      variant="filled"
    />
  )
}

// OpChip says what kind of write it was, grouped; the op name is the hover text.
export function OpChip({ op }: { op: string }) {
  const g = opGroup(op)
  return (
    <Chip
      size="small"
      data-type-chip={g}
      title={op}
      label={g}
      color={OP_COLOR[g]}
      variant="outlined"
    />
  )
}

const mono = { fontFamily: 'ui-monospace, monospace', fontSize: 13 } as const

// DiffLines shows the before and after of one write. It loads only when opened.
export function DiffLines({ id }: { id: string }) {
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

// ReceiptRow is one write on the Log page: what it did, why, from what, how to
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
        <ActorChip actor={r.actor} />
        <OpChip op={r.op} />
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          {r.op.replace(/_/g, ' ')}{' '}
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
