import { useState } from 'react'
import Box from '@mui/material/Box'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import Stack from '@mui/material/Stack'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import { useReceipt, useReceipts, type Receipt } from '@/api/receipts'
import { Section, StatusChip } from '@/components/Bits'
import { CopyButton } from '@/components/CopyButton'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { diffKind } from '@/lib/diff'
import { newestFirst, receiptTime, undoCommand } from '@/lib/receipts'
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
