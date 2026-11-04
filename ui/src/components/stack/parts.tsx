import type { ReactNode } from 'react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Link from '@mui/material/Link'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import type { useProposal } from '@/api/proposals'
import { StateChip, StatusChip } from '@/components/Bits'
import { diffKind } from '@/lib/diff'
import { linkParts, type Answer } from '@/lib/dossier'
import { UNTRUSTED_WORDS } from '@/lib/proposal'

// Pieces the item cards share.

export type ProposalData = NonNullable<ReturnType<typeof useProposal>['data']>
export type Item = ProposalData['items'][number]
type Claim = { text: string; source: string }

// CardProps is what every card gets: the item, the answer on screen (the draft, or the saved
// answer), and onAnswer, which is undefined when the owner may not change the answer.
export type CardProps = {
  it: Item
  answer: Answer | null
  onAnswer?: (a: Answer | null) => void
}

export const mono = { fontFamily: 'ui-monospace, monospace', fontSize: 13 } as const
export const block = {
  ...mono,
  my: 1,
  p: 1,
  bgcolor: 'action.hover',
  borderRadius: 1,
  whiteSpace: 'pre-wrap',
  overflowWrap: 'anywhere',
} as const

// ID_LIKE matches a document id, which a person does not read.
const ID_LIKE = /^[a-z][a-z_]*-[0-9a-z]{8,}$/

// LinkedText shows text with each web address as a link the owner can open to check it.
export function LinkedText({ text }: { text: string }) {
  return (
    <>
      {linkParts(text).map((p, i) =>
        p.url ? (
          <Link key={i} href={p.url} target="_blank" rel="noreferrer noopener">
            {p.text}
          </Link>
        ) : (
          <span key={i}>{p.text}</span>
        ),
      )}
    </>
  )
}

// Row is one labelled line of an item. It shows nothing when there is no text.
export function Row({ label, text }: { label: string; text?: string | null }) {
  if (!text) return null
  return (
    <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
      <Box component="span" sx={{ color: 'text.secondary' }}>
        {label}:{' '}
      </Box>
      <LinkedText text={text} />
    </Typography>
  )
}

export function ClaimRows({ label, list }: { label: string; list: Claim[] }) {
  return (
    <>
      {list.map((c) => (
        <Typography key={c.text} variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          <Box component="span" sx={{ color: 'text.secondary' }}>
            {label}:{' '}
          </Box>
          {c.text}
          {c.source && !ID_LIKE.test(c.source) && (
            <>
              {' '}
              [<LinkedText text={c.source} />]
            </>
          )}
        </Typography>
      ))}
    </>
  )
}

// Weighing is the case for and against an item.
export function Weighing({ it }: { it: Item }) {
  return (
    <Box sx={{ my: 1 }}>
      <ClaimRows label="Pro" list={it.pros ?? []} />
      <ClaimRows label="Con" list={it.cons ?? []} />
      {it.cost && <ClaimRows label="Cost" list={[it.cost]} />}
      {it.benefit && <ClaimRows label="Benefit" list={[it.benefit]} />}
      {!!it.confidence && (
        <Row
          label="Confidence"
          text={`${it.confidence}%${it.confidenceBasis ? `, because ${it.confidenceBasis}` : ''}`}
        />
      )}
      {!!it.recommendation && <Row label="Recommended" text={`${it.recommendation}%`} />}
      <Row label="Risk" text={it.risk} />
      <Row label="Reversible" text={[it.reversible, it.reversibleHow].filter(Boolean).join(': ')} />
      <Row label="Group" text={it.group} />
      <Row label="Runs after" text={it.after} />
      <Row label="Choice" text={it.choice} />
      <Row label="For" text={it.for} />
      <Row label="Not a duplicate of" text={it.notDuplicate} />
      <Row label="With results filled in" text={it.filled} />
    </Box>
  )
}

// EvidenceBox is the quote the item rests on and where it came from. A web address is a link.
export function EvidenceBox({ it }: { it: Item }) {
  return (
    <Box sx={{ borderLeft: 3, borderColor: 'divider', pl: 1.5, my: 1 }}>
      <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
        <Typography variant="caption" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
          Evidence
          {it.evidence.locator ? (
            <>
              {' ('}
              <LinkedText text={it.evidence.locator} />
              {')'}
            </>
          ) : null}
        </Typography>
        <Tooltip title={UNTRUSTED_WORDS}>
          <span>
            <StatusChip tone="attn" label="untrusted" />
          </span>
        </Tooltip>
      </Stack>
      <Typography variant="body2" sx={{ fontStyle: 'italic', overflowWrap: 'anywhere' }}>
        {it.evidence.quote}
      </Typography>
    </Box>
  )
}

export function DiffBlock({ lines, label }: { lines: string[]; label?: string }) {
  return (
    <Box
      component="pre"
      aria-label={label}
      sx={{ ...mono, m: 0, overflowX: 'auto', whiteSpace: 'pre-wrap' }}
    >
      {lines.map((l, n) => {
        const k = diffKind(l)
        return (
          <Box
            component="span"
            key={n}
            sx={{
              display: 'block',
              color: k === 'add' ? 'success.main' : k === 'del' ? 'error.main' : 'text.secondary',
            }}
          >
            {l}
          </Box>
        )
      })}
    </Box>
  )
}

// Preview is para's own account of a vault change: before, after, and what waits on it.
export function Preview({ pv }: { pv: NonNullable<Item['preview']> }) {
  return (
    <Box aria-label="Before and after" sx={{ my: 1 }}>
      {pv.error && <Alert severity="warning">This would fail now: {pv.error}</Alert>}
      {pv.changes.map((c) => (
        <Box key={c.doc}>
          <Typography variant="caption" color="text.secondary">
            {c.new ? 'New' : 'Changes'} {c.type}: {c.title || c.doc}
          </Typography>
          <DiffBlock lines={c.lines} />
        </Box>
      ))}
      {pv.downstream.map((x) => (
        <Row key={x.doc} label={x.title || x.doc} text={x.effect} />
      ))}
    </Box>
  )
}

// Thread shows the turns on an item: the owner's notes and the agent's replies to a comment.
export function Thread({ it }: { it: Item }) {
  const turns = it.thread ?? []
  if (turns.length === 0) return null
  return (
    <List dense disablePadding sx={{ my: 1 }} aria-label="Thread">
      {turns.map((n, i) => (
        <ListItem key={n.id || i} disableGutters sx={{ display: 'block' }}>
          {n.quote && (
            <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
              <em>&ldquo;{n.quote}&rdquo;</em>
            </Typography>
          )}
          {n.kind === 'reply' ? (
            <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
              {n.by || 'Agent'} replied: {n.text}
            </Typography>
          ) : (
            <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
              {n.kind.replace(/_/g, ' ')}: {n.text} <StateChip state={n.state} />
            </Typography>
          )}
          {n.answer && (
            <Typography variant="body2" color="text.secondary">
              Agent: {n.answer}
            </Typography>
          )}
        </ListItem>
      ))}
    </List>
  )
}

// DecisionBar is Approve, Reject, and (when the kind can be edited) Edit. Approve is one click,
// like accepting a spelling fix. A second click on the chosen button takes the choice back. The
// decision takes effect on Submit.
export function DecisionBar({
  answer,
  onAnswer,
  canEdit,
  onEdit,
  extra,
}: {
  answer: Answer | null
  onAnswer: (a: Answer | null) => void
  canEdit: boolean
  onEdit?: () => Answer
  extra?: ReactNode
}) {
  const d = answer?.decision ?? ''
  const pick = (decision: string, more?: Answer) => {
    if (d === decision) onAnswer({ ...answer, decision: null })
    else onAnswer({ ...answer, ...more, decision })
  }
  return (
    <Stack direction="row" spacing={1} useFlexGap sx={{ my: 1, flexWrap: 'wrap' }}>
      <Button
        size="small"
        color="success"
        variant={d === 'approve' ? 'contained' : 'outlined'}
        aria-pressed={d === 'approve'}
        onClick={() => pick('approve')}
      >
        Approve
      </Button>
      <Button
        size="small"
        color="error"
        variant={d === 'reject' ? 'contained' : 'outlined'}
        aria-pressed={d === 'reject'}
        onClick={() => pick('reject')}
      >
        Reject
      </Button>
      {canEdit && (
        <Button
          size="small"
          variant={d === 'edit' ? 'contained' : 'outlined'}
          aria-pressed={d === 'edit'}
          onClick={() => pick('edit', d === 'edit' ? undefined : onEdit?.())}
        >
          Edit, then approve
        </Button>
      )}
      {extra}
    </Stack>
  )
}

// CommentField is the owner's comment to the agent on one item.
export function CommentField({
  answer,
  onAnswer,
}: {
  answer: Answer | null
  onAnswer: (a: Answer | null) => void
}) {
  return (
    <TextField
      size="small"
      fullWidth
      multiline
      minRows={1}
      label="Comment for the agent"
      value={answer?.comment ?? ''}
      onChange={(e) => onAnswer({ ...answer, comment: e.target.value })}
      sx={{ mt: 1 }}
    />
  )
}
