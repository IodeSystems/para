import { useState } from 'react'
import { useRouteContext } from '@tanstack/react-router'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Link from '@mui/material/Link'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import Stack from '@mui/material/Stack'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import { useHistory, type HistoryFilter, type HistoryRow } from '@/api/history'
import { useReceiptRevert } from '@/api/receipts'
import { errorText } from '@/client/gqlClient'
import { canDecide } from '@/client/session'
import { Empty, Section, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { TextLink } from '@/components/links'
import { DiffLines } from '@/components/Receipts'
import { useToasts } from '@/components/ToastStack'
import { kindLabel, refLabel, refTarget, rowTime, whoLabel, whoTone } from '@/lib/history'
import { exactTime, humanTime } from '@/lib/when'

// Activity is the timeline of an item, newest first: what happened to it (made, edited, moved, a
// hand edit), what was said (notes entries, messages in and out, what the owner asked an agent and
// what the agent answered), and every agent write with its receipt. Conversation keeps only what
// was said. A receipt row opens its diff, and Undo puts the write back when the document has not
// changed since. A composer action refreshes it: the composer invalidates every query.
export function Activity({ id }: { id: string }) {
  const [filter, setFilter] = useState<HistoryFilter>('all')
  const q = useHistory(id, filter)
  const rows = q.data?.pages.flatMap((p) => p.items)
  return (
    <Section
      title="Activity"
      action={
        <ToggleButtonGroup
          exclusive
          size="small"
          value={filter}
          aria-label="Show"
          onChange={(_, v: HistoryFilter | null) => v && setFilter(v)}
        >
          <ToggleButton value="conversation" data-history-filter="conversation">
            Conversation
          </ToggleButton>
          <ToggleButton value="all" data-history-filter="all">
            Everything
          </ToggleButton>
        </ToggleButtonGroup>
      }
    >
      <Box data-activity={id}>
        <DataState q={{ ...q, data: rows }}>
          {(items) =>
            items.length === 0 ? (
              <Empty>
                {filter === 'conversation' ? 'Nothing was said about this yet.' : 'No activity.'}
              </Empty>
            ) : (
              <>
                <List dense disablePadding>
                  {items.map((r, i) => (
                    <ListItem
                      key={`${r.at}-${r.kind}-${r.ref ?? ''}-${i}`}
                      disableGutters
                      divider
                      sx={{ display: 'block' }}
                    >
                      <ActivityRow r={r} />
                    </ListItem>
                  ))}
                </List>
                {q.hasNextPage && (
                  <Button
                    size="small"
                    sx={{ mt: 1 }}
                    disabled={q.isFetchingNextPage}
                    onClick={() => void q.fetchNextPage()}
                  >
                    Show older
                  </Button>
                )}
              </>
            )
          }
        </DataState>
      </Box>
    </Section>
  )
}

// ActivityRow is one row: who, when (exact on hover), what, its text, and its links.
function ActivityRow({ r }: { r: HistoryRow }) {
  const t = rowTime(r)
  const target = refTarget(r)
  const [open, setOpen] = useState(false)
  return (
    <Box data-history-row={r.kind} data-who={r.who} sx={{ width: '100%' }}>
      <Stack
        direction="row"
        spacing={0.75}
        useFlexGap
        sx={{ alignItems: 'center', flexWrap: 'wrap' }}
      >
        <StatusChip tone={whoTone(r.who)} label={whoLabel(r)} />
        <Tooltip title={r.dateOnly ? t : exactTime(t)}>
          <Typography component="time" dateTime={t} variant="body2" color="text.secondary">
            {humanTime(t)}
          </Typography>
        </Tooltip>
        <Typography variant="body2" color="text.secondary">
          {kindLabel(r.kind)}
        </Typography>
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere', flex: '1 1 200px' }}>
          {r.summary}
        </Typography>
      </Stack>
      {r.text && r.text !== r.summary && (
        <Typography
          variant="body2"
          color="text.secondary"
          sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', mt: 0.25 }}
        >
          {r.kind === 'receipt' ? `Why: ${r.text}` : r.text}
        </Typography>
      )}
      <Stack
        direction="row"
        spacing={1}
        useFlexGap
        sx={{ alignItems: 'center', flexWrap: 'wrap', mt: 0.25 }}
      >
        {target && (
          <TextLink to={target.to} params={{ id: target.id }} underline="hover" variant="body2">
            Open {refLabel(r.kind)}
          </TextLink>
        )}
        {r.url && (
          <Link
            href={r.url}
            target="_blank"
            rel="noopener noreferrer"
            underline="hover"
            variant="body2"
          >
            Where it was sent
          </Link>
        )}
        {r.kind === 'receipt' && r.ref && r.undo && <UndoButton receipt={r.ref} />}
      </Stack>
      {r.kind === 'receipt' && r.ref && (
        <Box
          component="details"
          onToggle={(e) => setOpen(e.currentTarget.open)}
          sx={{ mt: 0.5, fontSize: '0.8125rem' }}
        >
          <Box component="summary" sx={{ cursor: 'pointer', width: 'fit-content' }}>
            What changed
          </Box>
          {open && <DiffLines id={r.ref} />}
        </Box>
      )}
    </Box>
  )
}

// UndoButton undoes one write from its receipt, as the owner. A toast says what happened; the
// server refuses it, with the reason, when a later write changed the document.
function UndoButton({ receipt }: { receipt: string }) {
  const { session } = useRouteContext({ from: '/_authed' })
  const revert = useReceiptRevert()
  const toasts = useToasts()
  if (!canDecide(session)) return null
  // mutateAsync, not mutate with callbacks: the refresh after the undo removes this button,
  // and the callbacks of an unmounted mutate never run.
  const run = async () => {
    try {
      await revert.mutateAsync(receipt)
      toasts.push({ text: 'Undid the write.' })
    } catch (e) {
      toasts.push({ text: `Could not undo: ${errorText(e)}`, tone: 'error' })
    }
  }
  return (
    <Button
      size="small"
      color="warning"
      data-history-undo={receipt}
      disabled={revert.isPending}
      onClick={() => void run()}
    >
      Undo
    </Button>
  )
}
