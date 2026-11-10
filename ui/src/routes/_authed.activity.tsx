import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import { useActivity, type ActivityItem } from '@/api/activity'
import { Empty, PageTitle, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { TextLink } from '@/components/links'
import { ACTIVITY_KINDS, ACTORS, activityKind, activityTarget, activityWho } from '@/lib/activity'
import { whoTone } from '@/lib/history'
import { exactTime, humanTime } from '@/lib/when'

export const Route = createFileRoute('/_authed/activity')({ component: Recent })

// Recent activity: what changed in the vault, newest first. Each row is when, who, what, the item,
// and why when an agent write has a receipt. Changes of one actor to one item within a minute are
// one row with a count.
function Recent() {
  const [actor, setActor] = useState('')
  const [type, setType] = useState('')
  const [since, setSince] = useState('')
  const q = useActivity({
    actor: actor || undefined,
    types: type ? [type] : undefined,
    since: since || undefined,
  })
  const rows = q.data?.pages.flatMap((p) => p.items)
  return (
    <>
      <PageTitle subtitle="What changed in the vault, newest first: who changed it, and why when an agent says.">
        Recent activity
      </PageTitle>
      <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap', mb: 2 }}>
        <TextField
          select
          size="small"
          label="Who"
          value={actor}
          onChange={(e) => setActor(e.target.value)}
          sx={{ flex: '1 1 150px' }}
        >
          {ACTORS.map((a) => (
            <MenuItem key={a.value} value={a.value}>
              {a.label}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          select
          size="small"
          label="What"
          value={type}
          onChange={(e) => setType(e.target.value)}
          sx={{ flex: '1 1 150px' }}
        >
          <MenuItem value="">Anything</MenuItem>
          {ACTIVITY_KINDS.map((k) => (
            <MenuItem key={k} value={k}>
              {activityKind(k)}
            </MenuItem>
          ))}
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
      </Stack>
      <Box data-activity-feed>
        <DataState q={{ ...q, data: rows }}>
          {(items) =>
            items.length === 0 ? (
              <Empty>Nothing changed that matches.</Empty>
            ) : (
              <>
                <List dense disablePadding>
                  {items.map((r, i) => (
                    <ListItem
                      key={`${r.at}-${r.doc}-${r.kind}-${i}`}
                      disableGutters
                      divider
                      sx={{ display: 'block' }}
                    >
                      <FeedRow r={r} />
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
    </>
  )
}

// FeedRow is one row: when (exact on hover), who, what, the item, the reason, and the diff.
function FeedRow({ r }: { r: ActivityItem }) {
  const target = activityTarget(r)
  return (
    <Box data-activity-row={r.kind} data-who={r.who} sx={{ width: '100%' }}>
      <Stack
        direction="row"
        spacing={0.75}
        useFlexGap
        sx={{ alignItems: 'center', flexWrap: 'wrap' }}
      >
        <StatusChip tone={whoTone(r.who)} label={activityWho(r)} />
        <Tooltip title={exactTime(r.at)}>
          <Typography component="time" dateTime={r.at} variant="body2" color="text.secondary">
            {humanTime(r.at)}
          </Typography>
        </Tooltip>
        <Typography variant="body2" color="text.secondary">
          {activityKind(r.kind)}
          {r.count > 1 ? ` (${r.count} changes)` : ''}
        </Typography>
        <TextLink
          to={target.to}
          params={{ id: target.id }}
          underline="hover"
          variant="body2"
          sx={{ overflowWrap: 'anywhere', flex: '1 1 200px' }}
        >
          {r.docName || r.doc}
        </TextLink>
      </Stack>
      {r.why && (
        <Typography
          variant="body2"
          color="text.secondary"
          sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', mt: 0.25 }}
        >
          Why: {r.why}
        </Typography>
      )}
      {r.receipt && (
        <TextLink to="/docs/$id" params={{ id: r.receipt }} underline="hover" variant="body2">
          Receipt
        </TextLink>
      )}
    </Box>
  )
}
