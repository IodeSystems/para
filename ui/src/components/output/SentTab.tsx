import { useState } from 'react'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Link from '@mui/material/Link'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import { reviewIo, type SentRow, useIoSent } from '@/api/output'
import { CHIP_ROW, Empty, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { TextLink } from '@/components/links'
import { useToasts } from '@/components/ToastStack'
import { errorText } from '@/client/gqlClient'
import { ioParty } from '@/lib/io'
import { resultChips, sinceFrom, variantWords } from '@/lib/output'
import { humanTime } from '@/lib/when'
import { useQueryClient } from '@tanstack/react-query'

export const SENT_MEDIUMS = [
  'email',
  'slack',
  'jira',
  'confluence',
  'github',
  'teams',
  'phone',
  'meeting',
  'verbal',
  'swivel',
  'other',
]

const SINCE_CHOICES = [
  { days: 0, label: 'Any time' },
  { days: 1, label: 'Last day' },
  { days: 7, label: 'Last 7 days' },
  { days: 30, label: 'Last 30 days' },
]

export type SentSearch = { medium?: string; to?: string; days?: number }

// SentTab is the history of what went out, newest first, with what each message became.
export function SentTab({
  search,
  onSearch,
}: {
  search: SentSearch
  onSearch: (p: SentSearch) => void
}) {
  const [toDraft, setToDraft] = useState(search.to ?? '')
  const q = useIoSent({
    medium: search.medium,
    to: search.to,
    since: sinceFrom(search.days ?? 0),
  })
  const toasts = useToasts()
  const qc = useQueryClient()
  const [picked, setPicked] = useState<string[]>([])
  const [busy, setBusy] = useState(false)

  const review = async (ids: string[]) => {
    if (ids.length === 0) return
    setBusy(true)
    try {
      const r = await reviewIo(ids)
      setPicked((p) => p.filter((id) => !ids.includes(id)))
      toasts.push({
        text: `Marked ${r.count} reviewed`,
        undo: r.undo
          ? async () => {
              await r.undo?.()
              await qc.invalidateQueries()
            }
          : undefined,
      })
      await qc.invalidateQueries()
    } catch (e) {
      toasts.push({ text: `Could not mark reviewed: ${errorText(e)}`, tone: 'error' })
    } finally {
      setBusy(false)
    }
  }

  const toggle = (id: string) =>
    setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]))

  return (
    <>
      <Stack
        direction="row"
        spacing={2}
        useFlexGap
        sx={{ mb: 2, alignItems: 'center', flexWrap: 'wrap' }}
      >
        <TextField
          select
          size="small"
          label="Medium"
          value={search.medium ?? ''}
          onChange={(e) => onSearch({ ...search, medium: e.target.value || undefined })}
          sx={{ minWidth: 140 }}
        >
          <MenuItem value="">Any</MenuItem>
          {SENT_MEDIUMS.map((m) => (
            <MenuItem key={m} value={m}>
              {m}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          size="small"
          label="To"
          value={toDraft}
          onChange={(e) => setToDraft(e.target.value)}
          onBlur={() => onSearch({ ...search, to: toDraft.trim() || undefined })}
          onKeyDown={(e) => {
            if (e.key === 'Enter') onSearch({ ...search, to: toDraft.trim() || undefined })
          }}
          sx={{ minWidth: 180 }}
        />
        <TextField
          select
          size="small"
          label="Since"
          value={search.days ?? 0}
          onChange={(e) => onSearch({ ...search, days: Number(e.target.value) || undefined })}
          sx={{ minWidth: 140 }}
        >
          {SINCE_CHOICES.map((c) => (
            <MenuItem key={c.days} value={c.days}>
              {c.label}
            </MenuItem>
          ))}
        </TextField>
        <Button
          size="small"
          variant="outlined"
          disabled={busy || picked.length === 0}
          onClick={() => void review(picked)}
        >
          Mark {picked.length || ''} reviewed
        </Button>
      </Stack>
      <DataState q={q}>
        {(rows) =>
          rows.length === 0 ? (
            <Empty>Nothing sent matches.</Empty>
          ) : (
            <List disablePadding>
              {rows.map((x) => (
                <SentRowItem
                  key={x.id}
                  x={x}
                  picked={picked.includes(x.id)}
                  busy={busy}
                  onPick={() => toggle(x.id)}
                  onReview={() => void review([x.id])}
                />
              ))}
            </List>
          )
        }
      </DataState>
    </>
  )
}

function SentRowItem({
  x,
  picked,
  busy,
  onPick,
  onReview,
}: {
  x: SentRow
  picked: boolean
  busy: boolean
  onPick: () => void
  onReview: () => void
}) {
  const chips = resultChips(x)
  const variant = variantWords(x.variant, x.of)
  return (
    <ListItem disableGutters divider sx={CHIP_ROW}>
      <Checkbox
        size="small"
        checked={picked}
        onChange={onPick}
        slotProps={{ input: { 'aria-label': `Select ${x.title || x.id}` } }}
      />
      <ListItemText
        primary={
          <TextLink to="/io/$id" params={{ id: x.id }} underline="hover">
            {x.title || 'Untitled message'}
          </TextLink>
        }
        secondary={
          <>
            {[x.to.map(ioParty).join(', ') && `to ${x.to.map(ioParty).join(', ')}`, humanTime(x.at)]
              .filter(Boolean)
              .join(' - ')}
            {variant && (
              <Typography component="span" variant="body2" color="text.secondary">
                {' - '}
                {variant}
                {x.textChanged ? ', edited before sending' : ''}
              </Typography>
            )}
            {!variant && x.textChanged && ' - edited before sending'}
          </>
        }
        slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
      />
      <StatusChip tone="quiet" label={x.medium} />
      {x.url && (
        <Link href={x.url} target="_blank" rel="noreferrer" variant="body2" sx={{ mr: 1 }}>
          link
        </Link>
      )}
      {chips.map((c) =>
        c.io ? (
          <TextLink key={c.key} to="/io/$id" params={{ id: c.io }} underline="none">
            <StatusChip tone={c.tone} label={c.label} />
          </TextLink>
        ) : (
          <Tooltip key={c.key} title={c.title ?? ''}>
            <span>
              <StatusChip tone={c.tone} label={c.label} />
            </span>
          </Tooltip>
        ),
      )}
      {x.reviewed ? (
        <StatusChip tone="quiet" label="Reviewed" />
      ) : (
        <Button size="small" disabled={busy} onClick={onReview}>
          Mark reviewed
        </Button>
      )}
    </ListItem>
  )
}
