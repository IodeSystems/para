import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Collapse from '@mui/material/Collapse'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import FormControlLabel from '@mui/material/FormControlLabel'
import LinearProgress from '@mui/material/LinearProgress'
import Radio from '@mui/material/Radio'
import RadioGroup from '@mui/material/RadioGroup'
import Stack from '@mui/material/Stack'
import Tab from '@mui/material/Tab'
import Tabs from '@mui/material/Tabs'
import TextField from '@mui/material/TextField'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import { useIODone, useIOApprove, useIoItem } from '@/api/io'
import {
  useApproveProposals,
  useDropProposal,
  useIoDiff,
  useIoRevisions,
  type ProposalDraft,
  type ProposalSet,
} from '@/api/proposals'
import { errorText } from '@/client/gqlClient'
import { StatusChip } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'
import { DraftEditor } from '@/components/DraftEditor'
import { LinkButton } from '@/components/links'
import { Markdown } from '@/components/Markdown'
import { DiffBlock } from '@/components/stack/parts'
import { Thread } from '@/components/Thread'
import { useToasts } from '@/components/ToastStack'
import { VoiceNote } from '@/components/Voice'
import { diffLines } from '@/lib/docs'
import {
  assessmentRows,
  canApprove,
  refusalText,
  selectedDraft,
  variationLabel,
} from '@/lib/proposals'
import { humanTime } from '@/lib/when'

// The draft card of an outgoing message. /next shows DraftReady (the text, Approve, Edit in place).
// The Output page shows ProposalCard: a variation set with the proposal's why, impact, assessments,
// history, and the owner's moves (Approve, Done by me, Drop, Discuss).

export const cardBox = {
  my: 1,
  p: 1.5,
  border: 1,
  borderColor: 'divider',
  borderRadius: 1,
  bgcolor: 'action.hover',
} as const

// DraftReady is an outgoing draft: its text, Approve (its exact text becomes ready to send), and
// Edit in place (Save, with "and approve" to approve the saved text in the same click).
export function DraftReady({
  id,
  decide,
  onGone,
}: {
  id: string
  decide: boolean
  onGone: (gone: boolean) => void
}) {
  const q = useIoItem(id)
  const approve = useIOApprove()
  const toasts = useToasts()
  const it = q.data?.item
  const approved = it?.state === 'ready'
  const [editing, setEditing] = useState(false)
  const run = async () => {
    try {
      await approve.mutateAsync(id)
      onGone(true)
      toasts.push({
        text: `Approved: ${it?.subject || it?.title || id}`,
        note: 'An agent sends it. Edit the draft to take the approval back.',
      })
    } catch (e) {
      toasts.push({ text: `Could not approve: ${errorText(e)}`, tone: 'error' })
    }
  }
  return (
    <Box sx={cardBox} data-next-draft={id}>
      <Typography variant="subtitle2" component="h3">
        Draft reply{it?.to?.length ? ` to ${it.to.join(', ')}` : ''}
      </Typography>
      {it?.subject && <Typography variant="body2">Subject: {it.subject}</Typography>}
      {editing && q.data ? (
        <DraftEditor
          key={q.data.doc.body}
          id={id}
          text={q.data.doc.body}
          approved={approved}
          onSaved={(approvedNow) => {
            setEditing(false)
            if (approvedNow) onGone(true)
            toasts.push({
              text: approvedNow
                ? `Saved and approved: ${it?.subject || it?.title || id}`
                : `Saved: ${it?.subject || it?.title || id}`,
              note: approvedNow ? 'An agent sends it.' : undefined,
            })
          }}
          onCancel={() => setEditing(false)}
        />
      ) : (
        <Typography
          component="div"
          variant="body2"
          sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', my: 1 }}
        >
          {q.data?.doc.body ?? ''}
        </Typography>
      )}
      {it && <VoiceNote item={it} />}
      <Stack direction="row" spacing={1}>
        {decide && !approved && (
          <Button
            size="small"
            variant="contained"
            color="success"
            data-act="approve"
            disabled={approve.isPending || !q.data}
            onClick={() => void run()}
          >
            {it?.voice === 'for_me' ? 'Approve as you' : 'Approve'}
          </Button>
        )}
        {approved && (
          <Typography variant="body2" color="text.secondary">
            Approved. An agent sends it.
          </Typography>
        )}
        {decide && !editing ? (
          <Button size="small" data-act="edit" disabled={!q.data} onClick={() => setEditing(true)}>
            Edit
          </Button>
        ) : (
          !decide && (
            <LinkButton size="small" data-act="edit" to="/io/$id" params={{ id }}>
              Edit
            </LinkButton>
          )
        )}
      </Stack>
    </Box>
  )
}

// AssessmentBars: confidence, recommend, and risk as small labelled percent bars; the basis is the
// tooltip. One that is not stated says so.
export function AssessmentBars({ d }: { d: Pick<ProposalDraft, 'assessments' | 'notStated'> }) {
  const rows = assessmentRows(d.assessments, d.notStated)
  return (
    <Stack spacing={0.75} sx={{ my: 1 }} data-assessments>
      {rows.map((r) => (
        <Tooltip
          key={r.name}
          title={r.percent === null ? 'The agent did not state it' : r.basis || 'No basis given'}
          placement="top-start"
        >
          <Box data-assessment={r.name} sx={{ maxWidth: 360 }}>
            <Typography
              variant="caption"
              color={r.percent === null ? 'text.disabled' : 'text.secondary'}
              component="div"
            >
              {r.text}
            </Typography>
            {r.percent !== null && (
              <LinearProgress
                variant="determinate"
                value={r.percent}
                color={r.name === 'risk' ? (r.percent >= 50 ? 'error' : 'warning') : 'primary'}
                aria-label={r.text}
                sx={{ height: 6, borderRadius: 3 }}
              />
            )}
          </Box>
        </Tooltip>
      ))}
    </Stack>
  )
}

// History lists the draft's revisions; picking one shows its text.
function History({ id, current }: { id: string; current: number }) {
  const [n, setN] = useState<number | undefined>()
  const list = useIoRevisions(id, undefined, true)
  const one = useIoRevisions(id, n, n !== undefined)
  return (
    <Box sx={{ my: 1 }} data-revisions={id}>
      {list.isError && <Alert severity="warning">{errorText(list.error)}</Alert>}
      <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap' }}>
        {(list.data?.revisions ?? []).map((r) => (
          <Tooltip key={r.n} title={`${r.by ? `${r.by}, ` : ''}${humanTime(r.at)}`}>
            <Chip
              size="small"
              clickable
              color={n === r.n ? 'primary' : 'default'}
              label={`r${r.n}${r.n === current ? ' (latest)' : ''}`}
              onClick={() => setN(n === r.n ? undefined : r.n)}
            />
          </Tooltip>
        ))}
      </Stack>
      {n !== undefined && one.data?.text != null && (
        <Box sx={{ mt: 1, p: 1, border: 1, borderColor: 'divider', borderRadius: 1 }}>
          <Typography variant="caption" color="text.secondary">
            Revision {n}
          </Typography>
          <Markdown source={one.data.text} />
        </Box>
      )}
    </Box>
  )
}

function DiffView({ id, revision }: { id: string; revision: number }) {
  const q = useIoDiff(id, revision - 1, revision, true)
  if (q.isError) return <Alert severity="warning">{errorText(q.error)}</Alert>
  if (!q.data) return null
  return (
    <>
      <Typography variant="caption" color="text.secondary" component="div" sx={{ mb: 0.5 }}>
        Revision {q.data.from} to {q.data.to}:{' '}
        {q.data.same ? 'no change' : `${q.data.added} added, ${q.data.removed} removed`}
      </Typography>
      {!q.data.same && (
        <DiffBlock lines={diffLines(q.data.parts)} label="Changes from the previous revision" />
      )}
    </>
  )
}

const attemptText = (a: NonNullable<ProposalDraft['lastAttempt']>) =>
  `Last attempt ${humanTime(a.at)}: ${a.result}${a.actor ? ` by ${a.actor}` : ''}${a.error ? `, ${a.error}` : ''}`

// DraftView is one draft of a proposal: header, text, why, impact, assessments, links, held, last
// attempt, revision and history.
function DraftView({ d, body }: { d: ProposalDraft; body?: string }) {
  const [showDiff, setShowDiff] = useState(false)
  const [showHistory, setShowHistory] = useState(false)
  const text = body ?? d.preview
  const to = d.to.join(', ')
  const links = [...(d.answers ? [{ ...d.answers, answers: true }] : []), ...d.links.map((l) => ({ ...l, answers: false }))]
  const working = d.thread.queued + d.thread.claimed
  return (
    <Box data-proposal-draft={d.id}>
      <Stack direction="row" spacing={1} useFlexGap sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
        <Typography variant="subtitle2" component="h3" sx={{ overflowWrap: 'anywhere' }}>
          {to ? `To ${to}` : 'No recipient'}
        </Typography>
        <Chip size="small" label={d.kind ? `${d.medium} / ${d.kind}` : d.medium} />
        {d.approval === 'approved' && <StatusChip tone="good" label="Approved" />}
        {d.approval === 'changed' && <StatusChip tone="attn" label="Changed since approved" />}
        {working > 0 && <StatusChip tone="wait" label="Agent working" />}
        <Chip
          size="small"
          variant="outlined"
          label={`rev ${d.revision}${d.revisions > 1 ? ` of ${d.revisions}` : ''}`}
        />
      </Stack>
      {d.subject && (
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          Subject: {d.subject}
        </Typography>
      )}
      <Box sx={{ my: 1 }}>
        {showDiff && d.revision > 1 ? (
          <DiffView id={d.id} revision={d.revision} />
        ) : (
          <Markdown source={text} />
        )}
      </Box>
      <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap' }}>
        {d.revision > 1 && (
          <Button size="small" data-act="diff" onClick={() => setShowDiff((v) => !v)}>
            {showDiff ? 'Show text' : 'Show changes'}
          </Button>
        )}
        {d.revisions > 1 && (
          <Button size="small" data-act="history" onClick={() => setShowHistory((v) => !v)}>
            {showHistory ? 'Hide history' : 'History'}
          </Button>
        )}
      </Stack>
      {showHistory && <History id={d.id} current={d.revision} />}
      <Typography variant="body2" sx={{ overflowWrap: 'anywhere', mt: 1 }}>
        <b>Why:</b> {d.why || <i>not stated</i>}
      </Typography>
      <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
        <b>Impact:</b> {d.impact || <i>not stated</i>}
      </Typography>
      <AssessmentBars d={d} />
      {links.length > 0 && (
        <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', my: 1 }}>
          {links.map((l) => (
            <Chip
              key={`${l.answers ? 'a' : 'l'}-${l.id}`}
              size="small"
              variant="outlined"
              label={
                <DocLink id={l.id}>
                  {l.answers ? 'Answers: ' : ''}
                  {l.title || l.id}
                </DocLink>
              }
            />
          ))}
        </Stack>
      )}
      {d.held && (
        <Alert severity="warning" sx={{ my: 1 }} data-held>
          Held: {d.held}
          {d.heldBy.length > 0 && (
            <>
              {' '}
              (by {d.heldBy.map((h, i) => (
                <span key={h}>
                  {i > 0 && ', '}
                  <DocLink id={h} />
                </span>
              ))})
            </>
          )}
        </Alert>
      )}
      {d.lastAttempt && (
        <Typography
          variant="caption"
          component="div"
          color={d.lastAttempt.result === 'failed' ? 'error' : 'text.secondary'}
          data-attempt
        >
          {attemptText(d.lastAttempt)}
        </Typography>
      )}
    </Box>
  )
}

// DoneDialog: "I sent or applied a variation myself". Pick the variation, the final text
// (prefilled with it), and where it is (optional). Para marks it sent; no agent does it again.
function DoneDialog({
  set,
  start,
  onClose,
}: {
  set: ProposalSet
  start: string
  onClose: () => void
}) {
  const [pick, setPick] = useState(start)
  const item = useIoItem(pick)
  const [edited, setEdited] = useState<{ id: string; text: string } | null>(null)
  const [url, setUrl] = useState('')
  const done = useIODone()
  const toasts = useToasts()
  const body = item.data?.doc.body ?? ''
  const text = edited && edited.id === pick ? edited.text : body
  const go = async () => {
    try {
      await done.mutateAsync({
        id: pick,
        text: text.trim() && text !== body ? text : undefined,
        url: url.trim() || undefined,
      })
      toasts.push({ text: 'Marked sent by you', note: 'No agent sends it again.' })
      onClose()
    } catch (e) {
      toasts.push({ text: `Could not mark done: ${errorText(e)}`, tone: 'error' })
    }
  }
  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="sm">
      <DialogTitle>I sent or applied a variation myself</DialogTitle>
      <DialogContent>
        <Stack spacing={1.5} sx={{ pt: 0.5 }}>
          {set.drafts.length > 1 && (
            <RadioGroup value={pick} onChange={(e) => setPick(e.target.value)}>
              {set.drafts.map((d, i) => (
                <FormControlLabel
                  key={d.id}
                  value={d.id}
                  control={<Radio size="small" />}
                  label={`${variationLabel(i, set.drafts.length)}: ${d.subject || d.preview.slice(0, 60)}`}
                />
              ))}
            </RadioGroup>
          )}
          <TextField
            size="small"
            multiline
            minRows={3}
            label="Final text, if you changed it (optional)"
            value={text}
            onChange={(e) => setEdited({ id: pick, text: e.target.value })}
          />
          <TextField
            size="small"
            label="Link to the result (optional)"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
          />
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="contained" disabled={done.isPending || !item.data} onClick={() => void go()}>
          Mark sent by me
        </Button>
      </DialogActions>
    </Dialog>
  )
}

function DropDialog({ id, onClose }: { id: string; onClose: () => void }) {
  const [reason, setReason] = useState('')
  const drop = useDropProposal()
  const toasts = useToasts()
  const go = async () => {
    try {
      await drop.mutateAsync({ id, reason: reason.trim() })
      toasts.push({ text: 'Dropped' })
      onClose()
    } catch (e) {
      toasts.push({ text: `Could not drop: ${errorText(e)}`, tone: 'error' })
    }
  }
  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="xs">
      <DialogTitle>Drop this draft</DialogTitle>
      <DialogContent>
        <TextField
          autoFocus
          fullWidth
          size="small"
          multiline
          minRows={2}
          sx={{ mt: 0.5 }}
          label="Why (required)"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
        />
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button
          variant="contained"
          color="error"
          disabled={drop.isPending || !reason.trim()}
          onClick={() => void go()}
        >
          Drop
        </Button>
      </DialogActions>
    </Dialog>
  )
}

// ProposalCard is one variation set: a tab per variation, the selected draft, and the owner's moves.
// Approve sends the draft_hash of the text on screen; a draft that changed since is refused.
// chosen and onChoose let the group's "Approve all shown" know which tab each set shows.
export function ProposalCard({
  set,
  chosen,
  onChoose,
  refusal,
  decide = true,
}: {
  set: ProposalSet
  chosen?: string
  onChoose: (id: string) => void
  // refusal is a batch refusal for the draft shown.
  refusal?: string
  decide?: boolean
}) {
  const d = selectedDraft(set, chosen)
  const item = useIoItem(d?.id ?? '')
  const approve = useApproveProposals()
  const toasts = useToasts()
  const [dialog, setDialog] = useState<'done' | 'drop' | null>(null)
  const [talk, setTalk] = useState(false)
  const [error, setError] = useState<string>()
  if (!d) return null
  const hash = item.data?.item.draftHash ?? undefined
  const open = canApprove(d)
  const run = async () => {
    if (!hash) return
    setError(undefined)
    try {
      await approve.mutateAsync([{ id: d.id, expectHash: hash }])
      toasts.push({
        text: `Approved: ${d.subject || d.title}`,
        note: set.drafts.length > 1 ? 'The other variations are closed.' : 'An agent sends it.',
      })
    } catch (e) {
      setError(refusalText(errorText(e)))
    }
  }
  const shown = error ?? refusal
  const closed = d.state === 'sent' || d.state === 'dropped' || d.state === 'superseded'
  return (
    <Box sx={cardBox} data-proposal-set={set.root}>
      {set.drafts.length > 1 && (
        <Tabs
          value={d.id}
          onChange={(_e, v: string) => {
            setError(undefined)
            onChoose(v)
          }}
          variant="scrollable"
          scrollButtons="auto"
          sx={{ mb: 1, minHeight: 36 }}
        >
          {set.drafts.map((x, i) => (
            <Tab
              key={x.id}
              value={x.id}
              sx={{ minHeight: 36, py: 0.5 }}
              label={`${variationLabel(i, set.drafts.length)}${x.needsYou ? ' •' : ''}`}
            />
          ))}
        </Tabs>
      )}
      <DraftView d={d} body={item.data?.doc.body} />
      {item.data && <VoiceNote item={item.data.item} />}
      {shown && (
        <Alert severity="error" sx={{ my: 1 }} data-refusal>
          {shown}
        </Alert>
      )}
      {decide && !closed && (
        <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap', mt: 1 }}>
          {open && (
            <Button
              size="small"
              variant="contained"
              color="success"
              data-act="approve"
              disabled={approve.isPending || !hash}
              onClick={() => void run()}
            >
              Approve
            </Button>
          )}
          <Button size="small" data-act="done" onClick={() => setDialog('done')}>
            Done by me
          </Button>
          <Button size="small" data-act="drop" onClick={() => setDialog('drop')}>
            Drop
          </Button>
          <LinkButton size="small" data-act="edit" to="/io/$id" params={{ id: d.id }}>
            Edit
          </LinkButton>
        </Stack>
      )}
      <Box sx={{ mt: 1 }}>
        <Button size="small" data-act="discuss" onClick={() => setTalk((v) => !v)}>
          {talk ? 'Hide discussion' : 'Discuss'}
          {d.thread.done + d.thread.queued + d.thread.claimed > 0
            ? ` (${d.thread.done + d.thread.queued + d.thread.claimed})`
            : ''}
        </Button>
        <Collapse in={talk} unmountOnExit>
          <Thread id={d.id} composer />
        </Collapse>
      </Box>
      {dialog === 'done' && (
        <DoneDialog set={set} start={d.id} onClose={() => setDialog(null)} />
      )}
      {dialog === 'drop' && <DropDialog id={d.id} onClose={() => setDialog(null)} />}
    </Box>
  )
}
