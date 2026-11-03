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
import { useLooking, useRequests } from '@/api/requests'
import { useToday } from '@/api/today'
import { CHIP_ROW, Empty, PageTitle, Section, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DatedAgenda } from '@/components/DatedAgenda'
import { DocLink, RfxLink } from '@/components/DocLink'
import { LinkButton, TextLink } from '@/components/links'
import { RowMenu } from '@/components/RowMenu'
import { AgentLooking, AgentWorking, findContextItem } from '@/components/FindContext'
import { TaskMenu } from '@/components/TaskMenu'
import { DueChip, TaskInline } from '@/components/TaskRows'
import { WaitingSection } from '@/components/Waiting'
import { joinNames, nameOr } from '@/lib/names'
import { toneOfSeverity } from '@/lib/status'
import { humanTime } from '@/lib/when'

export const Route = createFileRoute('/_authed/agenda')({
  validateSearch: (s: Record<string, unknown>): { by?: 'date' } => ({
    by: s.by === 'date' ? 'date' : undefined,
  }),
  component: Agenda,
})

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

type AgendaItem = Item & { kind?: string | null; why?: string | null; proposal?: boolean | null }

function AgendaLink({ x }: { x: AgendaItem }) {
  const title = x.title || nameOr('', x.id)
  if (x.kind === 'incident')
    return (
      <TextLink to="/incidents/$id" params={{ id: x.id }} underline="hover">
        {title}
      </TextLink>
    )
  if (x.kind === 'ask') return <RfxLink id={x.id}>{title}</RfxLink>
  return <DocLink id={x.id}>{title}</DocLink>
}

// AgendaRows is the one list of what needs you, in value order. Each row says why it is there.
// A question has the question actions; a task has Done and Later.
function AgendaRows({ items }: { items: AgendaItem[] }) {
  const looking = useLooking()
  return (
    <List dense disablePadding>
      {items.map((x) => (
        <ListItem key={x.id} disableGutters sx={CHIP_ROW} className="row-with-menu">
          <ListItemText
            primary={<AgendaLink x={x} />}
            secondary={x.why || undefined}
            slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
          />
          {x.kind && <StatusChip tone={x.kind === 'incident' ? 'bad' : 'quiet'} label={x.kind} />}
          <AgentLooking id={x.id} />
          <AgentWorking id={x.id} />
          {x.due && <DueChip due={x.due} />}
          {x.kind === 'question' && (
            <>
              <LinkButton
                size="small"
                variant="outlined"
                to="."
                search={(p) => ({ ...p, answer: x.id })}
              >
                Answer
              </LinkButton>
              <RowMenu
                label={`More actions for ${x.title || x.id}`}
                always
                items={[
                  {
                    label: 'Ask someone',
                    hint: 'Send this question to a person as an ask',
                    search: { ask: x.id },
                  },
                  findContextItem(x.id, looking.has(x.id)),
                  {
                    label: 'Later',
                    hint: 'Put this off. It still blocks what depends on it',
                    search: { later: x.id },
                  },
                  ...((x.blocks ?? 0) > 0
                    ? [
                        {
                          label: 'Cut a dependency',
                          hint: 'Let the work go ahead without this, and say why',
                          search: { cut: x.id },
                        },
                      ]
                    : []),
                  {
                    label: 'Drop',
                    hint: 'Close this question without answering it',
                    search: { drop: x.id },
                  },
                ]}
              />
            </>
          )}
          {x.kind === 'task' && (
            <>
              <TaskInline id={x.id} title={x.title || nameOr('', x.id)} />
              <TaskMenu id={x.id} title={x.title || x.id} always />
            </>
          )}
          {(x.kind === 'meeting' || x.kind === 'ask') && (
            <RowMenu
              label={`More actions for ${x.title || x.id}`}
              always
              items={[findContextItem(x.id, looking.has(x.id))]}
            />
          )}
        </ListItem>
      ))}
    </List>
  )
}

function Agenda() {
  const { by } = Route.useSearch()
  const q = useToday()
  const inbox = useInbox()
  const questions = useQuestions()
  const proposals = useOpenProposals()
  const requests = useRequests()
  const narrow = useMediaQuery('(max-width: 599px)')
  const [more, setMore] = useState<string>('')
  return (
    <>
      <PageTitle
        action={
          <Stack direction="row" spacing={1}>
            <LinkButton size="small" to="/meetings">
              Meetings
            </LinkButton>
            <LinkButton
              size="small"
              variant={by === 'date' ? 'contained' : 'outlined'}
              to="."
              search={(p) => ({ ...p, by: by === 'date' ? undefined : 'date' })}
              aria-pressed={by === 'date'}
            >
              Group by date
            </LinkButton>
          </Stack>
        }
      >
        Agenda
      </PageTitle>
      {by === 'date' ? (
        <DatedAgenda />
      ) : (
      <DataState q={q}>
        {(d) => {
          const t = d.today
          // An answer an agent proposed is to check, not to answer.
          const checks = (questions.data ?? []).filter((x) => x.hasProposal)
          const agenda: AgendaItem[] = t.agenda.items.filter((x) => !x.proposal)
          const agentWork = (requests.data ?? []).filter((x) => x.status === 'proposed')
          const inboxItems = inbox.data ?? []
          const props = proposals.open
          const need =
            props.length + t.answersToCheck + agentWork.length + agenda.length + inboxItems.length

          // The lead is the one thing to do first: a proposal, an answer to check, then the top
          // of the agenda, then a sort. It is not listed again.
          let lead: Lead | null = null
          let leadKind: 'proposal' | 'answers' | 'agenda' | 'inbox' | '' = ''
          const firstProp = props[0]
          const firstCheck = checks[0]
          const firstA = agenda[0]
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
          } else if (firstCheck) {
            leadKind = 'answers'
            lead = {
              why: `${t.answersToCheck} answer${t.answersToCheck === 1 ? '' : 's'} to check, from ${firstCheck.proposedBy || 'an agent'}`,
              title: <DocLink id={firstCheck.id}>{firstCheck.title}</DocLink>,
              action: (
                <LinkButton size="small" variant="contained" to="/review">
                  Check
                </LinkButton>
              ),
            }
          } else if (firstA) {
            leadKind = 'agenda'
            const action =
              firstA.kind === 'question' ? (
                <LinkButton
                  size="small"
                  variant="contained"
                  to="."
                  search={(p) => ({ ...p, answer: firstA.id })}
                >
                  Answer
                </LinkButton>
              ) : firstA.kind === 'task' ? (
                <LinkButton
                  size="small"
                  variant="contained"
                  to="."
                  search={(p) => ({ ...p, done: firstA.id })}
                >
                  Done
                </LinkButton>
              ) : firstA.kind === 'incident' ? (
                <LinkButton
                  size="small"
                  variant="contained"
                  to="/incidents/$id"
                  params={{ id: firstA.id }}
                >
                  Open
                </LinkButton>
              ) : null
            lead = {
              why: firstA.why || firstA.kind || 'First in order',
              title: <AgendaLink x={firstA} />,
              action,
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

          const checkRows = checks.slice(leadKind === 'answers' ? 1 : 0)
          const propRows = props.slice(leadKind === 'proposal' ? 1 : 0)
          const agendaRows = agenda.slice(leadKind === 'agenda' ? 1 : 0)
          const inboxRows = inboxItems.filter(
            (x) => !(leadKind === 'inbox' && x.id === firstIn?.id),
          )

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
          const rest: { key: string; label: string; count: number; body: ReactNode }[] = [
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
              <Section
                title="Approved, not done"
                count={t.approvedNotDone}
                hideEmpty
                action={
                  <LinkButton size="small" to="/review">
                    Open
                  </LinkButton>
                }
              >
                {null}
              </Section>
              <Section
                title="Answers to check"
                count={t.answersToCheck}
                hideEmpty
                action={
                  <LinkButton size="small" to="/review">
                    Check
                  </LinkButton>
                }
              >
                <List dense disablePadding>
                  {checkRows.map((x) => (
                    <ListItem key={x.id} disableGutters>
                      <ListItemText
                        primary={<DocLink id={x.id}>{x.title || nameOr('', x.id)}</DocLink>}
                        secondary={`proposed by ${x.proposedBy || 'an agent'}`}
                      />
                    </ListItem>
                  ))}
                </List>
              </Section>
              <Section
                title="Agent work to accept"
                count={agentWork.length}
                hideEmpty
                action={
                  <LinkButton size="small" to="/review">
                    Review
                  </LinkButton>
                }
              >
                <List dense disablePadding>
                  {agentWork.map((x) => (
                    <ListItem key={x.id} disableGutters>
                      <ListItemText
                        primary={<DocLink id={x.id}>{x.title || nameOr('', x.id)}</DocLink>}
                      />
                    </ListItem>
                  ))}
                </List>
              </Section>
              <Section
                title="What needs you, in order"
                count={agendaRows.length}
                hideEmpty
                action={
                  narrow ? undefined : (
                    <LinkButton size="small" to="/agenda">
                      Agenda
                    </LinkButton>
                  )
                }
              >
                <AgendaRows items={agendaRows} />
                {t.otherOpen > 0 && (
                  <LinkButton size="small" color="inherit" to="/questions">
                    {t.otherOpen} other open question{t.otherOpen === 1 ? '' : 's'}
                  </LinkButton>
                )}
                {t.deferred > 0 && (
                  <LinkButton size="small" color="inherit" to="/questions">
                    {t.deferred} put off (Later)
                  </LinkButton>
                )}
              </Section>
              {t.health > 0 && (
                <LinkButton size="small" color="inherit" to="/health">
                  {t.health} health finding{t.health === 1 ? '' : 's'}
                </LinkButton>
              )}
              {t.upgradeSteps > 0 && (
                <Typography variant="body2" color="text.secondary">
                  {t.upgradeSteps} vault upgrade step{t.upgradeSteps === 1 ? '' : 's'} pending. Run para vault upgrade status.
                </Typography>
              )}
              <Section title="Inbox to sort" count={inboxRows.length} hideEmpty>
                {inboxOf(inboxRows)}
              </Section>
              {need === 0 && <Empty>No task is ready.</Empty>}
            </>
          )
        }}
      </DataState>
      )}
    </>
  )
}
