import { useState, type ReactNode } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import useMediaQuery from '@mui/material/useMediaQuery'
import { useInbox } from '@/api/inbox'
import { useOpenProposals } from '@/api/proposals'
import { useQuestions } from '@/api/questions'
import { useToday } from '@/api/today'
import { CHIP_ROW, Empty, PageTitle, Section, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink, RfxLink } from '@/components/DocLink'
import { LinkButton, TextLink } from '@/components/links'
import { QuestionRows, scopeWords } from '@/components/Questions'
import { DueChip, TaskInline } from '@/components/TaskRows'
import { WaitingSection } from '@/components/Waiting'
import { joinNames, nameOr } from '@/lib/names'
import { toneOfSeverity } from '@/lib/status'
import { humanTime } from '@/lib/when'

export const Route = createFileRoute('/_authed/today')({ component: Today })

type Item = {
  id: string
  title: string
  due?: string | null
  detail?: string | null
  blocks?: number | null
  project?: string | null
  projectName?: string | null
  scope?: string | null
  scopeName?: string | null
  with?: string[] | null
}
type Kind = 'incident' | 'overdue' | 'nudge' | 'doc'

function ItemLink({ kind, item }: { kind: Kind; item: Item }) {
  const title = item.title || nameOr('', item.id)
  if (kind === 'incident')
    return (
      <TextLink to="/incidents/$id" params={{ id: item.id }} underline="hover">
        {title}
      </TextLink>
    )
  if (kind === 'nudge') return <RfxLink id={item.id}>{title}</RfxLink>
  return <DocLink id={item.id}>{title}</DocLink>
}

// aboutItem is the line under an item: who or what it is about, in names.
function aboutItem(kind: Kind, i: Item): string {
  const parts: string[] = []
  if (kind === 'nudge' && i.with?.length) parts.push(`ask ${joinNames(i.with)} again`)
  else if (i.with?.length) parts.push(`with ${joinNames(i.with)}`)
  if (i.project) parts.push(nameOr(i.projectName, i.project))
  if (kind === 'incident' && i.detail) parts.push(i.detail.split(' ').slice(1).join(' '))
  return parts.join(' - ')
}

function ItemRows({ kind, items, tasks }: { kind: Kind; items: Item[]; tasks?: boolean }) {
  return (
    <List dense disablePadding>
      {items.map((i) => (
        <ListItem key={i.id} disableGutters sx={CHIP_ROW}>
          <ListItemText
            primary={<ItemLink kind={kind} item={i} />}
            secondary={aboutItem(kind, i) || undefined}
            slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
          />
          {kind === 'incident' && i.detail && (
            <StatusChip
              tone={toneOfSeverity(i.detail.split(' ')[0] ?? '')}
              label={i.detail.split(' ')[0] ?? ''}
            />
          )}
          {i.due && <DueChip due={i.due} />}
          {tasks && <TaskInline id={i.id} title={i.title || nameOr('', i.id)} />}
        </ListItem>
      ))}
    </List>
  )
}

type Lead = { why: string; title: ReactNode; action: ReactNode }

function Today() {
  const q = useToday()
  const inbox = useInbox()
  const questions = useQuestions()
  const proposals = useOpenProposals()
  const narrow = useMediaQuery('(max-width: 599px)')
  const [more, setMore] = useState<string>('')
  return (
    <>
      <PageTitle>Today</PageTitle>
      <DataState q={q}>
        {(d) => {
          const t = d.today
          const qs = questions.data ?? []
          const inboxItems = inbox.data ?? []
          const incidents = t.incidents.items
          const overdue = t.overdue.items
          const nudges = t.nudges.items
          const props = proposals.open
          const need =
            props.length +
            incidents.length +
            qs.length +
            overdue.length +
            nudges.length +
            inboxItems.length

          // The lead is the one thing to do first: an incident, then a decision that blocks
          // work, then something late, then a follow-up, then a sort. It is not listed again.
          let lead: Lead | null = null
          let leadKind: 'proposal' | 'incident' | 'question' | 'overdue' | 'nudge' | 'inbox' | '' =
            ''
          const firstProp = props[0]
          const first = incidents[0]
          const firstQ = qs[0]
          const firstLate = overdue[0]
          const firstNudge = nudges[0]
          const firstIn = inboxItems[0]
          if (firstProp) {
            leadKind = 'proposal'
            lead = {
              why: `Proposal from ${firstProp.proposedBy}, ${firstProp.pending} of ${firstProp.itemCount} to decide`,
              title: (
                <TextLink to="/proposals/$id" params={{ id: firstProp.id }} underline="hover">
                  {firstProp.title || nameOr('', firstProp.id)}
                </TextLink>
              ),
              action: (
                <LinkButton
                  size="small"
                  variant="contained"
                  to="/proposals/$id"
                  params={{ id: firstProp.id }}
                >
                  Review
                </LinkButton>
              ),
            }
          } else if (first) {
            leadKind = 'incident'
            lead = {
              why: `Open incident${first.detail ? `, ${first.detail}` : ''}`,
              title: <ItemLink kind="incident" item={first} />,
              action: (
                <LinkButton
                  size="small"
                  variant="contained"
                  to="/incidents/$id"
                  params={{ id: first.id }}
                >
                  Open
                </LinkButton>
              ),
            }
          } else if (firstQ) {
            leadKind = 'question'
            lead = {
              why: `${firstQ.kind === 'decision' ? 'Decision' : 'Question'} ${scopeWords(firstQ.scopeType, firstQ.scopeName, firstQ.scope)}${firstQ.blocking > 0 ? `, blocks ${firstQ.blocking}` : ''}`,
              title: <DocLink id={firstQ.id}>{firstQ.title}</DocLink>,
              action: (
                <LinkButton
                  size="small"
                  variant="contained"
                  to="."
                  search={(p) => ({ ...p, answer: firstQ.id })}
                >
                  Answer
                </LinkButton>
              ),
            }
          } else if (firstLate) {
            leadKind = 'overdue'
            lead = {
              why: `Overdue${firstLate.due ? `, was due ${firstLate.due}` : ''}`,
              title: <ItemLink kind="overdue" item={firstLate} />,
              action: (
                <LinkButton
                  size="small"
                  variant="contained"
                  to="."
                  search={(p) => ({ ...p, done: firstLate.id })}
                >
                  Done
                </LinkButton>
              ),
            }
          } else if (firstNudge) {
            leadKind = 'nudge'
            lead = {
              why: 'Time to follow up',
              title: <ItemLink kind="nudge" item={firstNudge} />,
              action: (
                <LinkButton
                  size="small"
                  variant="contained"
                  to="/rfx/$id"
                  params={{ id: firstNudge.id }}
                >
                  Open
                </LinkButton>
              ),
            }
          } else if (firstIn) {
            leadKind = 'inbox'
            lead = {
              why: 'Captured, not sorted',
              title: <Box component="span">{firstIn.title || 'Untitled capture'}</Box>,
              action: (
                <LinkButton
                  size="small"
                  variant="contained"
                  to="."
                  search={(p) => ({ ...p, triage: firstIn.id })}
                >
                  Sort
                </LinkButton>
              ),
            }
          }

          // No item twice: everything shown is remembered, and later lists skip it.
          const seen = new Set<string>()
          const mark = (items: Item[]) => {
            const out = items.filter((i) => !seen.has(i.id))
            for (const i of out) seen.add(i.id)
            return out
          }
          if (leadKind === 'incident' && first) seen.add(first.id)
          if (leadKind === 'question' && firstQ) seen.add(firstQ.id)
          if (leadKind === 'overdue' && firstLate) seen.add(firstLate.id)
          if (leadKind === 'nudge' && firstNudge) seen.add(firstNudge.id)
          const propRows = props.slice(leadKind === 'proposal' ? 1 : 0)
          const incidentRows = mark(incidents)
          const questionRows = qs.filter((x) => {
            if (seen.has(x.id)) return false
            seen.add(x.id)
            return true
          })
          const overdueRows = mark(overdue)
          const inboxRows = inboxItems.filter(
            (x) => !(leadKind === 'inbox' && x.id === firstIn?.id),
          )
          const nudgeRows = mark(nudges)
          const soonRows = mark(t.dueSoon.items)
          const nextRows = d.next.filter((x) => !seen.has(x.id)).slice(0, 8)

          const inboxOf = (list: typeof inboxRows) => (
            <List dense disablePadding>
              {list.map((i) => (
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
                    primary={i.title || 'Untitled capture'}
                    secondary={humanTime(i.created)}
                    sx={{ pr: 8, overflowWrap: 'anywhere' }}
                  />
                </ListItem>
              ))}
            </List>
          )
          const nextList = (
            <List dense disablePadding>
              {nextRows.map((x) => (
                <ListItem key={x.id} disableGutters>
                  <ListItemText
                    primary={<DocLink id={x.id}>{x.title || nameOr('', x.id)}</DocLink>}
                    secondary={x.project ? nameOr(x.projectName, x.project) : undefined}
                  />
                  {x.due && <DueChip due={x.due} />}
                  <TaskInline id={x.id} title={x.title || nameOr('', x.id)} />
                </ListItem>
              ))}
            </List>
          )
          // On a phone the lower sections fold into the buttons at the bottom, so the page is short.
          const lower: { key: string; label: string; count: number; body: ReactNode }[] = narrow
            ? [
                {
                  key: 'follow',
                  label: 'Follow up',
                  count: nudgeRows.length,
                  body: <ItemRows kind="nudge" items={nudgeRows} />,
                },
                {
                  key: 'soon',
                  label: 'Due this week',
                  count: soonRows.length,
                  body: <ItemRows kind="doc" items={soonRows} tasks />,
                },
                { key: 'next', label: 'Next tasks', count: nextRows.length, body: nextList },
              ]
            : []
          const rest: { key: string; label: string; count: number; body: ReactNode }[] = [
            ...lower,
            {
              key: 'waiting',
              label: 'Waiting on others',
              count: t.waiting,
              body: <WaitingSection />,
            },
            {
              key: 'blocked',
              label: 'Blocked',
              count: t.blocked.count,
              body: <ItemRows kind="doc" items={t.blocked.items} />,
            },
            {
              key: 'due',
              label: 'Constraints to review',
              count: t.due.count,
              body: <ItemRows kind="doc" items={t.due.items} />,
            },
            {
              key: 'expiring',
              label: 'Policy exceptions expiring',
              count: t.expiring.count,
              body: <ItemRows kind="doc" items={t.expiring.items} />,
            },
          ].filter((r) => r.count > 0)

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
                  lost updates.
                </Alert>
              )}
              {need === 0 && (
                <Typography variant="body1" sx={{ mb: 1.5, fontWeight: 600 }}>
                  Nothing needs you right now.
                </Typography>
              )}
              {lead && (
                <Paper
                  variant="outlined"
                  sx={{ p: 1.5, mb: 2, borderLeft: 4, borderLeftColor: 'primary.main' }}
                >
                  <Typography variant="overline" color="text.secondary">
                    Start here - {lead.why}
                  </Typography>
                  <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                    <Typography
                      variant="h6"
                      component="p"
                      sx={{ flexGrow: 1, overflowWrap: 'anywhere' }}
                    >
                      {lead.title}
                    </Typography>
                    {lead.action}
                  </Stack>
                </Paper>
              )}
              {(rest.length > 0 || t.unverified > 0) && (
                <Box sx={{ mt: 1 }}>
                  <Stack
                    direction="row"
                    useFlexGap
                    sx={{ flexWrap: 'wrap', gap: 0.5 }}
                    aria-label="Also"
                  >
                    {rest.map((r) => (
                      <Button
                        key={r.key}
                        size="small"
                        color="inherit"
                        variant={more === r.key ? 'outlined' : 'text'}
                        aria-expanded={more === r.key}
                        onClick={() => setMore(more === r.key ? '' : r.key)}
                      >
                        {r.label} {r.count}
                      </Button>
                    ))}
                    {t.unverified > 0 && (
                      <LinkButton size="small" color="inherit" to="/policy">
                        Summaries to verify {t.unverified}
                      </LinkButton>
                    )}
                  </Stack>
                  {rest.find((r) => r.key === more)?.body}
                </Box>
              )}
              <Section title="Proposals" count={propRows.length} hideEmpty>
                <List dense disablePadding>
                  {propRows.map((x) => (
                    <ListItem key={x.id} disableGutters sx={CHIP_ROW}>
                      <ListItemText
                        primary={
                          <TextLink to="/proposals/$id" params={{ id: x.id }} underline="hover">
                            {x.title || nameOr('', x.id)}
                          </TextLink>
                        }
                        secondary={`${x.pending} of ${x.itemCount} to decide, from ${x.proposedBy}`}
                      />
                    </ListItem>
                  ))}
                </List>
              </Section>
              <Section title="Incidents" count={incidentRows.length} hideEmpty>
                <ItemRows kind="incident" items={incidentRows} />
              </Section>
              <Section title="Questions" count={questionRows.length} hideEmpty>
                <QuestionRows list={questionRows} />
              </Section>
              <Section title="Overdue" count={overdueRows.length} hideEmpty>
                <ItemRows kind="overdue" items={overdueRows} tasks />
              </Section>
              <Section title="Inbox to sort" count={inboxRows.length} hideEmpty>
                {inboxOf(inboxRows)}
              </Section>
              {!narrow && (
                <>
                  <Section title="Follow up" count={nudgeRows.length} hideEmpty>
                    <ItemRows kind="nudge" items={nudgeRows} />
                  </Section>
                  <Section title="Due this week" count={soonRows.length} hideEmpty>
                    <ItemRows kind="doc" items={soonRows} tasks />
                  </Section>
                  <Section title="Next tasks" count={nextRows.length} hideEmpty>
                    {nextList}
                  </Section>
                </>
              )}
              {need === 0 && nextRows.length === 0 && <Empty>No task is ready.</Empty>}
            </>
          )
        }}
      </DataState>
    </>
  )
}
