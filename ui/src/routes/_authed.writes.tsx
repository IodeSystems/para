import { createFileRoute } from '@tanstack/react-router'
import Box from '@mui/material/Box'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Typography from '@mui/material/Typography'
import { useOpenProposals, useUndone } from '@/api/proposals'
import { useReceipts } from '@/api/receipts'
import { Empty, PageTitle, Section, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { TextLink } from '@/components/links'
import { ReceiptCard } from '@/components/Receipts'
import { REVIEW_HINT } from '@/lib/proposal'
import { groupByDay } from '@/lib/receipts'
import { dateWords } from '@/lib/when'

export const Route = createFileRoute('/_authed/writes')({ component: Writes })

// Recent agent writes: what agents changed, newest first, by day and by run. The owner reviews
// here and acts in the chat; an undo is a command to copy.
function Writes() {
  return (
    <>
      <PageTitle subtitle="What agents changed, newest first. Ask your agent in the chat to change or undo anything.">
        Recent agent writes
      </PageTitle>
      <WaitingInChat />
      <ApprovedNotDone />
      <WriteList />
    </>
  )
}

// WaitingInChat lists the proposals the owner has not decided.
function WaitingInChat() {
  const q = useOpenProposals()
  return (
    <Section
      title="Waiting for your review"
      count={q.isError ? undefined : q.open.length}
      action={
        <TextLink to="/proposals" underline="hover" variant="body2">
          All proposals
        </TextLink>
      }
    >
      {q.open.length === 0 ? (
        <Empty>{q.isPending ? 'Loading.' : 'No proposal waits for you.'}</Empty>
      ) : (
        <>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 0.5 }}>
            {REVIEW_HINT}
          </Typography>
          <List dense disablePadding>
            {q.open.map((p) => (
              <ListItem key={p.id} disableGutters>
                <ListItemText
                  primary={
                    <TextLink to="/proposals/$id" params={{ id: p.id }} underline="hover">
                      {p.title || p.id}
                    </TextLink>
                  }
                  secondary={`${p.pending} to decide, by ${p.proposedBy}${p.sourceName ? `, from ${p.sourceName}` : ''}`}
                />
              </ListItem>
            ))}
          </List>
        </>
      )}
    </Section>
  )
}

// ApprovedNotDone lists outward items approved and not reported done.
function ApprovedNotDone() {
  const q = useUndone()
  const items = q.data ?? []
  return (
    <Section title="Approved, not done" count={items.length} hideEmpty>
      <List dense disablePadding>
        {items.map((u) => (
          <ListItem key={`${u.proposal}/${u.item}`} disableGutters>
            <ListItemText
              primary={
                <TextLink to="/proposals/$id" params={{ id: u.proposal }} underline="hover">
                  {u.summary || u.title}
                </TextLink>
              }
              secondary={[u.title, u.target].filter(Boolean).join(' - ')}
            />
          </ListItem>
        ))}
      </List>
    </Section>
  )
}

function WriteList() {
  const q = useReceipts({ limit: 200 })
  return (
    <DataState q={q}>
      {(rows) =>
        rows.length === 0 ? (
          <Empty>No agent has written here yet.</Empty>
        ) : (
          <>
            {groupByDay(rows).map((g) => (
              <Section key={g.day} title={dateWords(g.day)}>
                {g.runs.map((run, i) => (
                  <Box key={`${run.actor}-${i}`} sx={{ mb: 1 }} data-run={run.actor}>
                    <Typography
                      variant="subtitle2"
                      component="h3"
                      sx={{ overflowWrap: 'anywhere' }}
                    >
                      {run.actor}{' '}
                      {run.request && <StatusChip tone="quiet" label={`request ${run.request}`} />}
                    </Typography>
                    <List dense disablePadding>
                      {run.rows.map((r) => (
                        <ListItem key={r.id} disableGutters divider sx={{ display: 'block' }}>
                          <ReceiptCard r={r} showActor={false} />
                        </ListItem>
                      ))}
                    </List>
                  </Box>
                ))}
              </Section>
            ))}
          </>
        )
      }
    </DataState>
  )
}
