import { useState } from 'react'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import FormControlLabel from '@mui/material/FormControlLabel'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemButton from '@mui/material/ListItemButton'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useDependsSet, type DependsSetBody, type ItemContextData } from '@/api/context'
import { useSearch } from '@/api/search'
import { errorText } from '@/client/gqlClient'
import { Section, StateChip, StatusChip } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'
import { canWait, depActions, WAIT_TYPES } from '@/lib/depends'
import { hitKindLabel } from '@/lib/hitkind'

type Row = ItemContextData['waitsOn'][number]

// Dependencies are two sections under Context: Waits on (what the item waits on, a text reason
// too) and Blocks (the open items that wait on it). An optional one is shown with an "optional"
// chip: it is better done first but never holds the item. Each section is hidden when empty; an
// item that can wait keeps "Add what this waits on".
export function Dependencies({ c }: { c: ItemContextData }) {
  const set = useDependsSet()
  const [err, setErr] = useState('')
  const [adding, setAdding] = useState(false)
  const send = (body: DependsSetBody) => {
    setErr('')
    set.mutate(body, { onError: (e) => setErr(errorText(e)) })
  }
  const waits = canWait(c.type)
  const add = waits && (
    <Button size="small" onClick={() => setAdding((v) => !v)} aria-expanded={adding}>
      {adding ? 'Close' : 'Add what this waits on'}
    </Button>
  )
  return (
    <>
      {c.waitsOn.length > 0 ? (
        <Section title="Waits on" count={c.waitsOn.length} action={add}>
          <Rows item={c.id} side="waits" rows={c.waitsOn} busy={set.isPending} send={send} />
          {adding && <AddDep item={c.id} type={c.type} send={send} done={() => setAdding(false)} />}
        </Section>
      ) : (
        waits && (
          <Stack sx={{ mb: 2 }} data-depends-add>
            <span>{add}</span>
            {adding && (
              <AddDep item={c.id} type={c.type} send={send} done={() => setAdding(false)} />
            )}
          </Stack>
        )
      )}
      <Section title="Blocks" count={c.blocks.length} hideEmpty>
        <Rows item={c.id} side="blocks" rows={c.blocks} busy={set.isPending} send={send} />
      </Section>
      {err && (
        <Typography variant="caption" color="error" sx={{ display: 'block', mb: 2 }}>
          {err}
        </Typography>
      )}
    </>
  )
}

function Rows({
  item,
  side,
  rows,
  busy,
  send,
}: {
  item: string
  side: 'waits' | 'blocks'
  rows: Row[]
  busy: boolean
  send: (b: DependsSetBody) => void
}) {
  return (
    <List dense disablePadding data-depends={side}>
      {rows.map((r) => {
        const act = depActions(item, side, r)
        return (
          <ListItem
            key={`${r.via}-${r.id ?? r.reason}`}
            disableGutters
            divider
            sx={{ display: 'block' }}
          >
            <ListItemText
              primary={r.id ? <DocLink id={r.id}>{r.title}</DocLink> : r.title}
              slotProps={{ primary: { sx: { overflowWrap: 'anywhere' } } }}
            />
            <Stack
              direction="row"
              spacing={0.5}
              useFlexGap
              sx={{ flexWrap: 'wrap', alignItems: 'center', mt: 0.25 }}
            >
              {r.id ? (
                <StatusChip tone="quiet" label={hitKindLabel(r.type ?? '')} />
              ) : (
                <StatusChip tone="quiet" label="Reason" />
              )}
              {r.id && r.state && <StateChip state={r.state} />}
              {r.optional && <StatusChip tone="quiet" label="optional" />}
              {act?.toggle && (
                <Button size="small" disabled={busy} onClick={() => act.toggle && send(act.toggle)}>
                  {act.toggleLabel}
                </Button>
              )}
              {act && (
                <Button
                  size="small"
                  color="inherit"
                  disabled={busy}
                  onClick={() => send(act.remove)}
                >
                  Remove
                </Button>
              )}
            </Stack>
          </ListItem>
        )
      })}
    </List>
  )
}

// AddDep picks an item by search, or takes a text reason for a task.
function AddDep({
  item,
  type,
  send,
  done,
}: {
  item: string
  type: string
  send: (b: DependsSetBody) => void
  done: () => void
}) {
  const [q, setQ] = useState('')
  const [optional, setOptional] = useState(false)
  const [reason, setReason] = useState('')
  const hits = useSearch(q)
  const picks = (hits.data?.hits ?? [])
    .filter((h) => h.id !== item && [...WAIT_TYPES, 'project', 'request'].includes(h.kind))
    .slice(0, 8)
  return (
    <Stack spacing={1} sx={{ mt: 1 }} data-depends-picker>
      <TextField
        size="small"
        label="Find the item it waits on"
        value={q}
        onChange={(e) => setQ(e.target.value)}
        autoFocus
      />
      <FormControlLabel
        control={
          <Checkbox
            size="small"
            checked={optional}
            onChange={(e) => setOptional(e.target.checked)}
          />
        }
        label="Optional: better before, but it does not hold this item"
      />
      {picks.length > 0 && (
        <List dense disablePadding>
          {picks.map((h) => (
            <ListItem key={h.id} disablePadding>
              <ListItemButton
                onClick={() => {
                  send({ id: item, on: h.id, optional })
                  done()
                }}
              >
                <ListItemText
                  primary={h.title || h.id}
                  secondary={hitKindLabel(h.kind)}
                  slotProps={{ primary: { sx: { overflowWrap: 'anywhere' } } }}
                />
              </ListItemButton>
            </ListItem>
          ))}
        </List>
      )}
      {q.trim() !== '' && hits.isSuccess && picks.length === 0 && (
        <Typography variant="body2" color="text.secondary">
          No item matches.
        </Typography>
      )}
      {type === 'task' && (
        <Stack
          direction="row"
          spacing={1}
          useFlexGap
          sx={{ flexWrap: 'wrap', alignItems: 'center' }}
        >
          <TextField
            size="small"
            label="Or a reason in words"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            sx={{ flexGrow: 1, minWidth: 0 }}
          />
          <Button
            size="small"
            disabled={reason.trim() === ''}
            onClick={() => {
              send({ id: item, reason: reason.trim() })
              done()
            }}
          >
            Add reason
          </Button>
        </Stack>
      )}
    </Stack>
  )
}
