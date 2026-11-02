import { createFileRoute } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useProposals } from '@/api/proposals'
import { useAccept, useReopen, useReview } from '@/api/review'
import { Empty, PageTitle, Section } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { TextLink } from '@/components/links'
import { StateChip } from '@/components/Bits'
import { errorText } from '@/client/gqlClient'
import { ago, newestTS, transition } from '@/lib/events'

export const Route = createFileRoute('/_authed/review')({ component: Review })

function ProposalsSection() {
  const q = useProposals()
  // A closed proposal has been decided. Everything else is waiting on someone.
  const items = (q.data ?? []).filter((p) => p.status !== 'closed' || p.error)
  return (
    <Section title="Proposals" count={items.length}>
      {q.isError ? (
        <Alert severity="error">{errorText(q.error)}</Alert>
      ) : items.length === 0 ? (
        <Empty>No agent proposal waits for you.</Empty>
      ) : (
        <List dense disablePadding>
          {items.map((p) => (
            <ListItem key={p.id} disableGutters>
              <ListItemText
                primary={
                  <TextLink to="/proposals/$id" params={{ id: p.id }} underline="hover">
                    {p.title || p.id}
                  </TextLink>
                }
                secondary={
                  p.error
                    ? p.error
                    : `revision ${p.revision} - ${p.pending} of ${p.itemCount} open - by ${p.proposedBy}`
                }
                slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
              />
              {p.error ? (
                <StateChip state="blocked" label="broken file" />
              ) : (
                <StateChip state={p.status} />
              )}
            </ListItem>
          ))}
        </List>
      )}
    </Section>
  )
}

function Review() {
  const q = useReview()
  const accept = useAccept()
  const reopen = useReopen()
  const err = accept.error ?? reopen.error
  return (
    <>
      <PageTitle
        action={
          <Stack direction="row" spacing={1}>
            <Button
              variant="outlined"
              size="small"
              disabled={!q.data?.canUndo || reopen.isPending}
              onClick={() => reopen.mutate()}
            >
              Reopen last
            </Button>
            <Button
              variant="contained"
              size="small"
              disabled={!q.data?.count || accept.isPending}
              onClick={() => accept.mutate('')}
            >
              Accept all
            </Button>
          </Stack>
        }
      >
        Review
      </PageTitle>
      {err && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {errorText(err)}
        </Alert>
      )}
      <ProposalsSection />
      <DataState q={q}>
        {(r) => (
          <>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
              {r.count} change{r.count === 1 ? '' : 's'} by others
              {r.position ? ` since you accepted ${ago(r.position)}` : ''}. Your own CLI, TUI, and
              browser work is not listed.
            </Typography>
            <Section title="Needs you" count={r.needs.length}>
              {r.needs.length === 0 ? (
                <Empty>Nothing waits for you.</Empty>
              ) : (
                <List dense disablePadding>
                  {r.needs.map((n) => (
                    <ListItem key={`${n.kind}-${n.id}`} disableGutters>
                      <ListItemText
                        primary={<DocLink id={n.id}>{n.title || n.id}</DocLink>}
                        secondary={n.kind}
                      />
                    </ListItem>
                  ))}
                </List>
              )}
            </Section>
            {r.groups.length === 0 ? (
              <Empty>Nothing changed since the last accept.</Empty>
            ) : (
              r.groups.map((g) => (
                <Section
                  key={g.actor}
                  title={g.actor || 'unknown actor'}
                  count={g.events.length}
                  action={
                    <Button
                      size="small"
                      disabled={accept.isPending}
                      onClick={() => accept.mutate(newestTS(g.events))}
                    >
                      Accept up to here
                    </Button>
                  }
                >
                  <List dense disablePadding>
                    {[...g.events].reverse().map((e) => (
                      <ListItem key={`${e.ts}-${e.doc}-${e.op}`} disableGutters>
                        <ListItemText
                          primary={
                            <>
                              {e.op} <DocLink id={e.doc} />
                            </>
                          }
                          secondary={[transition(e), ago(e.ts), `via ${e.surface}`]
                            .filter(Boolean)
                            .join(' - ')}
                        />
                      </ListItem>
                    ))}
                  </List>
                </Section>
              ))
            )}
          </>
        )}
      </DataState>
    </>
  )
}
