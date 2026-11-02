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
import { CHIP_ROW, Empty, PageTitle, Section } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { TextLink } from '@/components/links'
import { StateChip } from '@/components/Bits'
import { errorText } from '@/client/gqlClient'
import { humanTime } from '@/lib/when'
import { needWords, newestTS, opPhrase, transition } from '@/lib/events'
import { nameOr, stripIds } from '@/lib/names'

export const Route = createFileRoute('/_authed/review')({ component: Review })

function ProposalsSection() {
  const q = useProposals()
  // A closed proposal has been decided. Everything else is waiting on someone.
  const items = (q.data ?? []).filter((p) => p.status !== 'closed' || p.error)
  return (
    <Section title="Proposals" count={q.isError ? undefined : items.length} hideEmpty>
      {q.isError ? (
        <Alert severity="error">{errorText(q.error)}</Alert>
      ) : (
        <List dense disablePadding>
          {items.map((p) => (
            <ListItem key={p.id} disableGutters sx={CHIP_ROW}>
              <ListItemText
                primary={
                  <TextLink to="/proposals/$id" params={{ id: p.id }} underline="hover">
                    {p.title || nameOr('', p.id)}
                  </TextLink>
                }
                secondary={
                  p.error
                    ? p.error
                    : [
                        p.sourceName && `from ${p.sourceName}`,
                        `${p.pending} of ${p.itemCount} to decide`,
                        `by ${p.proposedBy}`,
                        p.revision > 1 && `revision ${p.revision}`,
                      ]
                        .filter(Boolean)
                        .join(' - ')
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
            {q.data?.canUndo && (
              <Button
                variant="outlined"
                size="small"
                disabled={reopen.isPending}
                onClick={() => reopen.mutate()}
              >
                Undo last mark
              </Button>
            )}
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
              {r.count} logged change{r.count === 1 ? '' : 's'} by others
              {r.position
                ? ` since you last marked changes reviewed (${humanTime(r.position)})`
                : ' since the start'}
              . Your own CLI, TUI, and browser work is not listed. Proposals are decided item by
              item; marking changes reviewed does not touch them.
            </Typography>
            <Section title="Needs you" count={r.needs.length} hideEmpty>
              {
                <List dense disablePadding>
                  {r.needs.map((n) => (
                    <ListItem key={`${n.kind}-${n.id}`} disableGutters>
                      <ListItemText
                        primary={
                          <DocLink id={n.id}>{stripIds(n.title) || nameOr('', n.id)}</DocLink>
                        }
                        secondary={needWords(n.kind)}
                      />
                    </ListItem>
                  ))}
                </List>
              }
            </Section>
            {r.groups.length === 0 ? (
              <Empty>Nothing changed since you last marked changes reviewed.</Empty>
            ) : (
              <Stack direction="row" sx={{ mb: 1, justifyContent: 'flex-end' }}>
                <Button
                  variant="contained"
                  size="small"
                  disabled={accept.isPending}
                  onClick={() => accept.mutate('')}
                >
                  Mark all {r.count} logged changes reviewed
                </Button>
              </Stack>
            )}
            {r.groups.length > 0 &&
              r.groups.map((g) => (
                <Section
                  key={g.actor}
                  title={g.actor || 'unknown actor'}
                  count={g.events.length}
                  action={
                    r.groups.length > 1 ? (
                      <Button
                        size="small"
                        disabled={accept.isPending}
                        onClick={() => accept.mutate(newestTS(g.events))}
                      >
                        Mark reviewed up to here
                      </Button>
                    ) : undefined
                  }
                >
                  <List dense disablePadding>
                    {[...g.events].reverse().map((e) => (
                      <ListItem key={`${e.ts}-${e.doc}-${e.op}`} disableGutters>
                        <ListItemText
                          primary={
                            <>
                              {opPhrase(e.op)}{' '}
                              <DocLink id={e.doc}>{e.docName || nameOr('', e.doc)}</DocLink>
                            </>
                          }
                          secondary={[
                            e.op === 'create' ? (e.to ? `in ${e.to}` : '') : transition(e),
                            humanTime(e.ts),
                          ]
                            .filter(Boolean)
                            .join(' - ')}
                        />
                      </ListItem>
                    ))}
                  </List>
                </Section>
              ))}
          </>
        )}
      </DataState>
    </>
  )
}
