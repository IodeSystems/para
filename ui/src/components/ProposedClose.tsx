import { useState } from 'react'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useApproveClose, usePreparedClose } from '@/api/preparedClose'
import { revertReceipt } from '@/api/receipts'
import { useThreadPost } from '@/api/thread'
import { errorText } from '@/client/gqlClient'
import { SourceList } from '@/components/AnswerCheck'
import { LinkButton } from '@/components/links'
import { useToasts } from '@/components/ToastStack'
import { cardBox } from '@/components/output/ProposalCard'
import { closeLine, isHighClose, notItQuote } from '@/lib/proposedClose'

// ProposedClose is the close an agent proposed for a task, on the Next card and the task page: one
// line "Likely done, 90%: why", the evidence, Approve close (one call, undoable), Edit on the task
// page, and Not it, which sends the owner's words to the agent on the item's thread. A confidence
// of 80 or more gets the accent; a lower one shows plainly. It shows only while the proposal is
// ready: an edit of the task after it hides it.
export function ProposedClose({
  id,
  title,
  decide,
  onGone,
  edit = true,
}: {
  id: string
  title: string
  decide: boolean
  onGone?: (gone: boolean) => void
  edit?: boolean
}) {
  const q = usePreparedClose(id)
  const approve = useApproveClose()
  const post = useThreadPost()
  const toasts = useToasts()
  const [notIt, setNotIt] = useState(false)
  const [words, setWords] = useState('')
  const p = q.data
  if (!p?.proposed || !p.ready || !p.close) return null
  const c = p.close
  const high = isHighClose(c.confidence)
  const ev = c.evidence
  const onApprove = async () => {
    try {
      const r = await approve.mutateAsync(id)
      onGone?.(true)
      const receipt = r.receipt
      toasts.push({
        text: `Closed (${r.outcome}): ${title}`,
        target: { id },
        undo: receipt
          ? async () => {
              await revertReceipt(receipt)
              onGone?.(false)
            }
          : undefined,
        note: receipt ? 'Undo opens it again.' : 'This close left no log entry to undo.',
      })
    } catch (e) {
      toasts.push({ text: `Could not close: ${errorText(e)}`, tone: 'error' })
    }
  }
  const onSend = async () => {
    try {
      await post.mutateAsync({ id, text: words.trim(), kind: 'redirect', quote: notItQuote(c) })
      setWords('')
      setNotIt(false)
      toasts.push({ text: `Sent to the agent: ${title}`, target: { id } })
    } catch (e) {
      toasts.push({ text: `Could not send: ${errorText(e)}`, tone: 'error' })
    }
  }
  return (
    <Box
      data-proposed-close={id}
      data-high={high ? 'yes' : undefined}
      sx={{
        ...cardBox,
        ...(high && {
          borderColor: 'primary.main',
          borderLeft: 4,
          borderLeftColor: 'primary.main',
        }),
      }}
    >
      <Typography variant="subtitle2" component="h3">
        Proposed close{p.by ? `, from ${p.by}` : ''}
      </Typography>
      <Typography
        sx={{ overflowWrap: 'anywhere', my: 0.5, fontWeight: high ? 600 : undefined }}
        color={high ? 'primary' : undefined}
      >
        {closeLine(c)}
      </Typography>
      {ev?.locator ? (
        <SourceList sources={[{ ref: ev.locator, quote: ev.quote }]} />
      ) : ev?.via ? (
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          {ev.via.source} {ev.via.ref}: {ev.verify}
        </Typography>
      ) : (
        <Typography variant="body2" color="text.secondary">
          No evidence: the confidence is held low.
        </Typography>
      )}
      {c.confidenceBasis && (
        <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
          Basis: {c.confidenceBasis}
        </Typography>
      )}
      <Stack direction="row" spacing={1} useFlexGap sx={{ mt: 1, flexWrap: 'wrap' }}>
        {decide && (
          <Button
            size="small"
            variant="contained"
            color={high ? 'primary' : 'inherit'}
            data-act="approve"
            disabled={approve.isPending}
            onClick={() => void onApprove()}
          >
            Approve close
          </Button>
        )}
        {edit && (
          <LinkButton size="small" data-act="edit" to="/docs/$id" params={{ id }}>
            Edit
          </LinkButton>
        )}
        {decide && (
          <Button size="small" data-act="not-it" onClick={() => setNotIt((v) => !v)}>
            Not it
          </Button>
        )}
      </Stack>
      {notIt && (
        <Stack spacing={1} sx={{ mt: 1 }}>
          <TextField
            size="small"
            multiline
            minRows={2}
            label="What is wrong with it? Sent to the agent"
            value={words}
            onChange={(e) => setWords(e.target.value)}
            slotProps={{ htmlInput: { 'data-not-it-words': id } }}
          />
          <Stack direction="row" spacing={1}>
            <Button
              size="small"
              variant="contained"
              data-act="send-not-it"
              disabled={!words.trim() || post.isPending}
              onClick={() => void onSend()}
            >
              Send to agent
            </Button>
            <Button size="small" onClick={() => setNotIt(false)}>
              Cancel
            </Button>
          </Stack>
        </Stack>
      )}
    </Box>
  )
}
