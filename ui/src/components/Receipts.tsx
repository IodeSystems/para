import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useReceipt, useReceiptCheck, useReceiptUndo, useReceipts } from '@/api/receipts'
import { errorText } from '@/client/gqlClient'
import { Section, StateChip, StatusChip } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'

// diffKind names the kind of a diff line from its first characters.
function diffKind(line: string): 'add' | 'del' | 'same' {
  if (line.startsWith('+ ')) return 'add'
  if (line.startsWith('- ')) return 'del'
  return 'same'
}

type Row = NonNullable<ReturnType<typeof useReceipts>['data']>[number]

// ReceiptCard shows what one agent write changed, why, and how to verify, with the owner's word.
function ReceiptCard({ r }: { r: Row }) {
  const [open, setOpen] = useState(false)
  const [wrong, setWrong] = useState(false)
  const [note, setNote] = useState('')
  const q = useReceipt(r.id, open)
  const check = useReceiptCheck()
  const revert = useReceiptUndo()
  const err = check.error ?? revert.error
  const d = q.data
  return (
    <Box data-receipt={r.id} sx={{ width: '100%' }}>
      <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
        {r.actor}: {r.op.replace(/_/g, ' ')} <DocLink id={r.doc}>{r.docName || r.title}</DocLink>{' '}
        <StateChip state={r.check} />{' '}
        {r.selfReported && <StatusChip tone="wait" label="self-reported" />}
      </Typography>
      {r.why && (
        <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
          Why: {r.why}
        </Typography>
      )}
      {err && <Alert severity="error">{errorText(err)}</Alert>}
      <Stack direction="row" spacing={1} sx={{ my: 0.5 }}>
        <Button size="small" onClick={() => setOpen(!open)}>
          {open ? 'Hide' : 'What changed'}
        </Button>
        {r.check === 'unchecked' && (
          <>
            <Button size="small" onClick={() => check.mutate({ id: r.id, verdict: 'verified' })}>
              Verified
            </Button>
            <Button size="small" color="inherit" onClick={() => setWrong(true)}>
              Wrong
            </Button>
          </>
        )}
      </Stack>
      {wrong && (
        <Stack direction="row" spacing={1} sx={{ my: 0.5 }}>
          <TextField
            size="small"
            label="What is wrong"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            fullWidth
          />
          <Button
            size="small"
            disabled={note.trim() === ''}
            onClick={() => check.mutate({ id: r.id, verdict: 'wrong', note: note.trim() })}
          >
            Send
          </Button>
        </Stack>
      )}
      {open && d && (
        <Box aria-label="What this write changed">
          {(d.receipt.sources ?? []).length > 0 && (
            <Typography variant="body2">Sources: {d.receipt.sources?.join(', ')}</Typography>
          )}
          {d.receipt.verify && (
            <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
              Verify: {d.receipt.verify}
            </Typography>
          )}
          <Box
            component="pre"
            sx={{
              m: 0,
              fontFamily: 'ui-monospace, monospace',
              fontSize: 13,
              whiteSpace: 'pre-wrap',
              overflowWrap: 'anywhere',
            }}
          >
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
          {d.revertible && (
            <Button size="small" color="inherit" onClick={() => revert.mutate({ id: r.id })}>
              Revert this write
            </Button>
          )}
        </Box>
      )}
    </Box>
  )
}

// ReceiptSection lists agent writes: on a document page, those to it; on Review, those unchecked.
export function ReceiptSection({
  title,
  doc,
  check,
}: {
  title: string
  doc?: string
  check?: 'unchecked'
}) {
  const q = useReceipts({ doc, check, limit: 30 })
  const items = q.data ?? []
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
