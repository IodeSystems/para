import { createFileRoute } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import { LinkButton } from '@/components/links'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import { useInbox } from '@/api/inbox'
import { useToday } from '@/api/today'
import { errorText } from '@/client/gqlClient'
import { Empty, PageTitle, Section, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink, RfxLink } from '@/components/DocLink'

export const Route = createFileRoute('/_authed/today')({ component: Today })

type Items = { count: number; items: { id: string; title: string }[] }

function DocList({ g, empty }: { g: Items; empty: string }) {
  if (g.items.length === 0) return <Empty>{empty}</Empty>
  return (
    <List dense disablePadding>
      {g.items.map((i) => (
        <ListItem key={i.id} disableGutters>
          <ListItemText primary={<DocLink id={i.id}>{i.title || i.id}</DocLink>} />
        </ListItem>
      ))}
    </List>
  )
}

function InboxSection() {
  const q = useInbox()
  const items = q.data ?? []
  return (
    <Section title="Inbox" count={items.length}>
      {items.length === 0 ? (
        <Empty>{q.isError ? errorText(q.error) : 'Nothing to sort.'}</Empty>
      ) : (
        <List dense disablePadding>
          {items.map((i) => (
            <ListItem
              key={i.id}
              disableGutters
              secondaryAction={
                <LinkButton size="small" to="." search={(p) => ({ ...p, triage: i.id })}>
                  Sort
                </LinkButton>
              }
            >
              <ListItemText
                primary={i.title || i.id}
                secondary={i.created}
                sx={{ pr: 8, overflowWrap: 'anywhere' }}
              />
            </ListItem>
          ))}
        </List>
      )}
    </Section>
  )
}

function Today() {
  const q = useToday()
  return (
    <>
      <PageTitle>Today</PageTitle>
      <DataState q={q}>
        {(d) => {
          const t = d.today
          return (
            <>
              {t.error && (
                <Alert severity="error" sx={{ mb: 2 }}>
                  {t.error}
                </Alert>
              )}
              {(t.syncConflicts.length > 0 || t.lostUpdates.length > 0) && (
                <Alert severity="warning" sx={{ mb: 2 }}>
                  File sync left {t.syncConflicts.length} conflict copies and {t.lostUpdates.length}{' '}
                  lost updates. Run lint to see them.
                </Alert>
              )}
              <InboxSection />
              <Section title="Next tasks" count={d.next.length}>
                {d.next.length === 0 ? (
                  <Empty>No task is ready.</Empty>
                ) : (
                  <List dense disablePadding>
                    {d.next.slice(0, 8).map((x) => (
                      <ListItem key={x.id} disableGutters>
                        <ListItemText
                          primary={<DocLink id={x.id}>{x.title || x.id}</DocLink>}
                          secondary={x.project || undefined}
                        />
                        <StateChip state={x.status} />
                      </ListItem>
                    ))}
                  </List>
                )}
              </Section>
              <Section title="Waiting on others" count={d.waiting.length}>
                {d.waiting.length === 0 ? (
                  <Empty>Nobody owes you an answer.</Empty>
                ) : (
                  <List dense disablePadding>
                    {d.waiting.map((x) => (
                      <ListItem key={x.id} disableGutters>
                        <ListItemText
                          primary={<RfxLink id={x.id}>{x.title || x.id}</RfxLink>}
                          secondary={
                            [x.route, x.nextNudge && `nudge ${x.nextNudge}`]
                              .filter(Boolean)
                              .join(' - ') || undefined
                          }
                        />
                        <Stack direction="row" spacing={0.5}>
                          {x.stalled && <StateChip state="stalled" />}
                          {x.due && <StateChip state="blocked" label="nudge due" />}
                        </Stack>
                      </ListItem>
                    ))}
                  </List>
                )}
              </Section>
              <Section title="Blocked" count={t.blocked.count}>
                <DocList g={t.blocked} empty="Nothing is blocked." />
              </Section>
              <Section title="Constraints due for review" count={t.due.count}>
                <DocList g={t.due} empty="No constraint is due." />
              </Section>
              <Section title="Nudges due" count={t.nudges.count}>
                <DocList g={t.nudges} empty="No nudge is due." />
              </Section>
              <Section title="Policy exceptions expiring" count={t.expiring.count}>
                <DocList g={t.expiring} empty="None expire soon." />
              </Section>
              <Section title="Unverified policy summaries" count={t.unverified}>
                <Empty>A summary no person has reviewed shows as unverified.</Empty>
              </Section>
            </>
          )
        }}
      </DataState>
    </>
  )
}
