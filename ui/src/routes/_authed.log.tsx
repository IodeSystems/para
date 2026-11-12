import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import { useReceipts } from '@/api/receipts'
import { Empty, PageTitle } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { OutcomesSection, ReceiptRow } from '@/components/Receipts'
import { emptyToUndefined, newestFirst, type CheckState } from '@/lib/receipts'

export const Route = createFileRoute('/_authed/log')({ component: Log })

// Log: every write, newest first, with its reason and sources. The owner checks a
// write (verified or wrong) or undoes it here.
function Log() {
  const [actor, setActor] = useState('')
  const [op, setOp] = useState('')
  const [check, setCheck] = useState<CheckState | ''>('')
  const [since, setSince] = useState('')
  const [until, setUntil] = useState('')
  const filter = {
    actor: emptyToUndefined(actor),
    op: emptyToUndefined(op),
    check: check || undefined,
    since: emptyToUndefined(since),
    until: emptyToUndefined(until),
  }
  const q = useReceipts({ ...filter, limit: 200 })
  return (
    <>
      <PageTitle subtitle="What was changed, and by whom, newest first. Check a write, or undo it.">
        Log
      </PageTitle>
      <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap', mb: 2 }}>
        <TextField
          size="small"
          label="Actor"
          value={actor}
          onChange={(e) => setActor(e.target.value)}
          sx={{ flex: '1 1 130px' }}
        />
        <TextField
          size="small"
          label="Op"
          value={op}
          onChange={(e) => setOp(e.target.value)}
          sx={{ flex: '1 1 130px' }}
        />
        <TextField
          select
          size="small"
          label="Check"
          value={check}
          onChange={(e) => setCheck(e.target.value as CheckState | '')}
          sx={{ flex: '1 1 130px' }}
        >
          <MenuItem value="">Any</MenuItem>
          <MenuItem value="unchecked">Unchecked</MenuItem>
          <MenuItem value="verified">Verified</MenuItem>
          <MenuItem value="wrong">Wrong</MenuItem>
        </TextField>
        <TextField
          size="small"
          type="date"
          label="Since"
          value={since}
          onChange={(e) => setSince(e.target.value)}
          slotProps={{ inputLabel: { shrink: true } }}
          sx={{ flex: '1 1 150px' }}
        />
        <TextField
          size="small"
          type="date"
          label="Until"
          value={until}
          onChange={(e) => setUntil(e.target.value)}
          slotProps={{ inputLabel: { shrink: true } }}
          sx={{ flex: '1 1 150px' }}
        />
      </Stack>
      <DataState q={q}>
        {(rows) =>
          rows.length === 0 ? (
            <Empty>No log entry matches.</Empty>
          ) : (
            <List dense disablePadding sx={{ mb: 2 }}>
              {newestFirst(rows).map((r) => (
                <ListItem key={r.id} disableGutters divider sx={{ display: 'block' }}>
                  <ReceiptRow r={r} undone={!!r.undone} />
                </ListItem>
              ))}
            </List>
          )
        }
      </DataState>
      <OutcomesSection since={filter.since} until={filter.until} />
    </>
  )
}
