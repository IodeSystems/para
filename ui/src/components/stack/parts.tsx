import { useState, type ReactNode } from 'react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Link from '@mui/material/Link'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import type { useProposal } from '@/api/proposals'
import { StateChip, StatusChip } from '@/components/Bits'
import { Markdown } from '@/components/Markdown'
import { diffKind, newDoc, splitDiff, type NewDoc } from '@/lib/diff'
import { useDoc } from '@/api/doc'
import { DocLink } from '@/components/DocLink'
import { evidenceCheck, linkParts, type Answer } from '@/lib/dossier'
import { UNTRUSTED_WORDS } from '@/lib/proposal'
import { noteLabel, noteProblem, parseRefs, withDecision, type Decision } from '@/lib/review'

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

// verifyAsk is the note Send to agent starts with when evidence gives no way to verify it.
export const verifyAsk = (item: string) =>
  `Add a link or a way to verify the evidence for ${item}`

// SourceName is the title of a source document, or its id.
function SourceName({ id }: { id: string }) {
  const q = useDoc(id)
  return <DocLink id={id}>{q.data?.title || id}</DocLink>
}

// EvidenceWhere says where the evidence is: a web address opens in a new tab, a vault document in
// the app, a private connector item as the agent's sentence on how to find it.
function EvidenceWhere({ it }: { it: Item }) {
  const c = evidenceCheck(it.evidence)
  switch (c.kind) {
    case 'url':
      return (
        <Link href={c.url} target="_blank" rel="noreferrer noopener">
          {c.url}
        </Link>
      )
    case 'vault':
      return <DocLink id={c.id}>{it.evidence.locator}</DocLink>
    case 'via':
      return (
        <>
          {c.verify} (<SourceName id={c.source} />)
        </>
      )
  }
  return <>{it.evidence.locator}</>
}

// EvidenceBox is the quote the item rests on and where it came from. Evidence with no way to
// verify it is marked, and onAsk starts a Send to agent note that asks for one.
export function EvidenceBox({ it, onAsk }: { it: Item; onAsk?: () => void }) {
  const none = evidenceCheck(it.evidence).kind === 'none'
  return (
    <Box sx={{ borderLeft: 3, borderColor: none ? 'warning.main' : 'divider', pl: 1.5, my: 1 }}>
      <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
        <Typography variant="caption" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
          Evidence
          {it.evidence.locator ? (
            <>
              {' ('}
              <EvidenceWhere it={it} />
              {')'}
            </>
          ) : null}
        </Typography>
        {none && <StatusChip tone="attn" label="no way to verify" />}
        {none && onAsk && (
          <Button size="small" onClick={onAsk}>
            Ask for a link
          </Button>
        )}
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

// DiffBlock draws diff lines, one per row, monospace: added green, removed red.
export function DiffBlock({ lines, label }: { lines: string[]; label?: string }) {
  return (
    <Box role="group" aria-label={label} sx={{ ...mono, m: 0, overflowX: 'auto' }}>
      {splitDiff(lines).map((l, n) => {
        const k = diffKind(l)
        return (
          <Box
            key={n}
            data-diff={k}
            sx={{
              whiteSpace: 'pre-wrap',
              overflowWrap: 'anywhere',
              minHeight: '1.4em',
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

// NewDocBlock shows a new document as its fields and its rendered body.
function NewDocBlock({ doc }: { doc: NewDoc }) {
  return (
    <Box sx={{ ...block, my: 0.5 }}>
      <Box component="ul" aria-label="Fields" sx={{ m: 0, pl: 2.5 }}>
        {doc.fields.map((f) => (
          <Typography component="li" variant="body2" key={f.key} sx={{ overflowWrap: 'anywhere' }}>
            <strong>{f.key}: </strong>
            {f.value}
          </Typography>
        ))}
      </Box>
      {doc.body && (
        <Box sx={{ mt: 1, fontFamily: 'inherit' }}>
          <Markdown source={doc.body} />
        </Box>
      )}
    </Box>
  )
}

// Preview is para's own account of a vault change: before, after, and what waits on it.
export function Preview({ pv }: { pv: NonNullable<Item['preview']> }) {
  return (
    <Box aria-label="Before and after" sx={{ my: 1 }}>
      {pv.error && <Alert severity="warning">This would fail now: {pv.error}</Alert>}
      {pv.changes.map((c) => {
        const nd = c.new ? newDoc(c.lines) : null
        return (
          <Box key={c.doc}>
            <Typography variant="caption" color="text.secondary">
              {c.new ? 'New' : 'Changes'} {c.type}: {c.title || c.doc}
            </Typography>
            {nd ? <NewDocBlock doc={nd} /> : <DiffBlock lines={c.lines} />}
          </Box>
        )
      })}
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

// focusNote puts the cursor in a note field once it is drawn.
export function focusNote(id: string) {
  window.setTimeout(() => document.getElementById(id)?.focus(), 0)
}

// noteIdOf is the id of the note field of an item card.
export const noteIdOf = (item: string) => `note-${item}`

// ActionBar is the same row of actions on every card: Approve, Edit, Decline, Skip, and Send to agent.
// A question has no Approve, Edit, or Decline: the owner answers it. Approve is one click, like
// accepting a spelling fix; a second click on the chosen button takes the choice back. Decline and
// Send to agent put the cursor in the note, which they need. The decision takes effect on Submit.
export function ActionBar({
  answer,
  onAnswer,
  question,
  start,
  noteId,
  extra,
}: {
  answer: Answer | null
  onAnswer: (a: Answer | null) => void
  question: boolean
  start?: Answer
  noteId: string
  extra?: ReactNode
}) {
  const d = answer?.decision ?? ''
  const pick = (decision: Decision) => {
    onAnswer(withDecision(answer, decision, start))
    if (d !== decision && (decision === 'reject' || decision === 'ask' || decision === 'skip'))
      focusNote(noteId)
  }
  const button = (decision: Decision, label: string, color?: 'success' | 'error') => (
    <Button
      size="small"
      color={color}
      data-act={decision}
      variant={d === decision ? 'contained' : 'outlined'}
      aria-pressed={d === decision}
      onClick={() => pick(decision)}
    >
      {label}
    </Button>
  )
  return (
    <Stack direction="row" spacing={1} useFlexGap sx={{ my: 1, flexWrap: 'wrap' }}>
      {!question && button('approve', 'Approve', 'success')}
      {!question && start && button('edit', 'Edit, then approve')}
      {!question && button('reject', 'Decline', 'error')}
      {button('skip', 'Skip')}
      {button('ask', 'Send to agent')}
      {extra}
    </Stack>
  )
}

// NoteField is the owner's note on one item: why they decline, what the agent should do, or a
// comment. A decline and an ask need it. With Send to agent the owner may name vault documents the
// agent should read.
export function NoteField({
  answer,
  onAnswer,
  id,
}: {
  answer: Answer | null
  onAnswer: (a: Answer | null) => void
  id: string
}) {
  const d = answer?.decision
  const problem = noteProblem(d, answer?.comment)
  return (
    <>
      <TextField
        size="small"
        fullWidth
        multiline
        minRows={1}
        id={id}
        label={noteLabel(d)}
        value={answer?.comment ?? ''}
        error={problem !== ''}
        helperText={problem || undefined}
        onChange={(e) => onAnswer({ ...answer, comment: e.target.value })}
        sx={{ mt: 1 }}
      />
      {d === 'ask' && (
        <RefsField refs={answer?.refs ?? []} onRefs={(refs) => onAnswer({ ...answer, refs })} />
      )}
    </>
  )
}

// RefsField takes vault ids for the agent to read, separated by commas. It reads the ids when the
// field loses focus, so the owner can type freely.
export function RefsField({ refs, onRefs }: { refs: string[]; onRefs: (refs: string[]) => void }) {
  const [text, setText] = useState(refs.join(', '))
  return (
    <TextField
      size="small"
      fullWidth
      label="Documents for the agent to read (vault ids, optional)"
      value={text}
      onChange={(e) => {
        setText(e.target.value)
        onRefs(parseRefs(e.target.value))
      }}
      sx={{ mt: 1 }}
    />
  )
}

// ReviewStateChip marks an item the owner skipped or sent back to an agent.
export function ReviewStateChip({ state }: { state: string }) {
  if (state === 'skipped')
    return <Chip size="small" data-review="skipped" label="skipped" variant="filled" />
  if (state === 'sent_back')
    return (
      <Chip
        size="small"
        data-review="sent_back"
        color="info"
        label="sent back to the agent"
        variant="filled"
      />
    )
  return null
}

export type DoneFields = { url: string; locator: string; text: string; note: string }

// DoneForm is the inline form of Done by me: where the result is, the final text, and a note. It
// is part of the page, not a browser dialog. Confirm takes effect at once.
export function DoneForm({
  start,
  textLabel,
  withNote,
  busy,
  error,
  onConfirm,
  onCancel,
}: {
  start: string
  textLabel: string
  withNote: boolean
  busy: boolean
  error?: string
  onConfirm: (f: DoneFields) => void
  onCancel: () => void
}) {
  const [f, setF] = useState<DoneFields>({ url: '', locator: '', text: start, note: '' })
  const set = (k: keyof DoneFields) => (e: { target: { value: string } }) =>
    setF((x) => ({ ...x, [k]: e.target.value }))
  return (
    <Box
      data-done-form
      role="group"
      aria-label="Done by me"
      sx={{ my: 1, p: 1.5, border: 1, borderColor: 'divider', borderRadius: 1 }}
    >
      <Typography variant="body2" sx={{ mb: 1 }}>
        You did this yourself. Para marks it done, and no agent does it again.
      </Typography>
      <Stack spacing={1}>
        <TextField
          size="small"
          autoFocus
          label="Link to the result (optional)"
          value={f.url}
          onChange={set('url')}
        />
        <TextField
          size="small"
          label="Where it is, with no link (optional)"
          value={f.locator}
          onChange={set('locator')}
        />
        <TextField
          size="small"
          multiline
          minRows={2}
          label={textLabel}
          value={f.text}
          onChange={set('text')}
        />
        {withNote && (
          <TextField size="small" label="Note (optional)" value={f.note} onChange={set('note')} />
        )}
      </Stack>
      {error && (
        <Alert severity="error" sx={{ mt: 1 }}>
          {error}
        </Alert>
      )}
      <Stack direction="row" spacing={1} sx={{ mt: 1 }}>
        <Button size="small" variant="contained" disabled={busy} onClick={() => onConfirm(f)}>
          Confirm done
        </Button>
        <Button size="small" onClick={onCancel}>
          Cancel
        </Button>
      </Stack>
    </Box>
  )
}

// BodyFileBox shows the text of the vault file an item's body names, beside the item, read only.
export function BodyFileBox({ file }: { file: NonNullable<Item['bodyFile']> }) {
  return (
    <Box data-body-file={file.path} sx={{ my: 1 }}>
      <Typography variant="caption" color="text.secondary" component="div">
        The body is the file {file.path}
        {file.title ? ` (${file.title})` : ''}. Its text:
      </Typography>
      <Box
        component="pre"
        aria-label="Text of the file"
        sx={{ ...block, maxHeight: 320, overflow: 'auto' }}
      >
        {file.text}
      </Box>
    </Box>
  )
}
