import { useState, type ReactNode } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import ListSubheader from '@mui/material/ListSubheader'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import useMediaQuery from '@mui/material/useMediaQuery'
import { useIoList } from '@/api/io'
import { useQuestions } from '@/api/questions'
import { useRequests } from '@/api/requests'
import { useToday } from '@/api/today'
import { CHIP_ROW, Empty, PageTitle, Section, StatusChip } from '@/components/Bits'
import { AgendaRow } from '@/components/AgendaActions'
import { DataState } from '@/components/DataState'
import { DatedAgenda } from '@/components/DatedAgenda'
import { DocLink, RfxLink } from '@/components/DocLink'
import { LinkButton, TextLink } from '@/components/links'
import { AgentLooking, AgentWorking } from '@/components/FindContext'
import { DueChip } from '@/components/TaskRows'
import { WaitingSection } from '@/components/Waiting'
import { OptionalLine, SkippedLine } from '@/components/MeetingAttend'
import { PoolsDue } from '@/components/Pools'
import { joinNames, nameOr } from '@/lib/names'
import { toneOfSeverity } from '@/lib/status'
import { humanTime } from '@/lib/when'
import { openLine } from '@/lib/sent'
import { readyTarget, readyWords } from '@/lib/ready'

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
      <TextLink to="/doc/$id" params={{ id: item.id }} underline="hover">
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

// ItemRows lists a group of the agenda. rowKind gives its rows Done and Later (blocked tasks);
// constraints and exceptions to review have none.
function ItemRows({ kind, items, rowKind }: { kind: Kind; items: Item[]; rowKind?: string }) {
  return (
    <List dense disablePadding>
      {items.map((i) => (
        <AgendaRow key={i.id} x={{ id: i.id, title: i.title, kind: rowKind }}>
          {(actions) => (
            <ListItem disableGutters sx={CHIP_ROW}>
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
              {rowKind && actions}
            </ListItem>
          )}
        </AgendaRow>
      ))}
    </List>
  )
}

type Lead = {
  why: string
  title: ReactNode
  action: ReactNode | null
  // row is the agenda item the lead shows, so it carries the actions of its kind.
  row?: AgendaItem
}

type AgendaItem = Item & {
  kind?: string | null
  why?: string | null
  bucket?: string | null
  group?: string | null
  ready?: boolean | null
  readyKind?: string | null
  readyRef?: string | null
}

// ReadyChip marks a row an agent prepared and that has not changed since. One click opens what was
// prepared: the draft reply, or the item with its proposed answer or plan.
function ReadyChip({ x }: { x: AgendaItem }) {
  if (!x.ready) return null
  return (
    <Box component="span" data-ready={x.id}>
      <DocLink id={readyTarget(x)}>
        <StatusChip tone="good" label={readyWords(x.readyKind)} />
      </DocLink>
    </Box>
  )
}

type OrderSummary = { reply: number; replyMinutes: number; replyPrep: number }

// groupHeading names a group of the value order where it starts: the reply batch, focus, and
// the rest after them. Other groups keep no heading.
function groupHeading(group: string, last: string, s: OrderSummary): string {
  if (group === 'owed') return 'Owed: promises to people, and their prep'
  if (group === 'reply')
    return `Reply: ${s.reply} waiting on you, about ${s.replyMinutes} min, ${s.replyPrep} an agent can prepare`
  if (group === 'focus') return 'Focus'
  if (group === 'recheck') return 'Recheck'
  if (
    group === 'impact' &&
    (last === 'owed' || last === 'reply' || last === 'focus' || last === 'recheck')
  )
    return 'Then'
  return ''
}

function AgendaLink({ x }: { x: AgendaItem }) {
  const title = x.title || nameOr('', x.id)
  if (x.kind === 'incident')
    return (
      <TextLink to="/doc/$id" params={{ id: x.id }} underline="hover">
        {title}
      </TextLink>
    )
  if (x.kind === 'ask') return <RfxLink id={x.id}>{title}</RfxLink>
  return <DocLink id={x.id}>{title}</DocLink>
}

// AgendaRows is the one list of what needs you, in value order. Each row says why it is there.
// The owner acts on a row in the chat. Replies come as one batch, then focus. What cannot start
// yet comes last, under its own heading.
function AgendaRows({ items, summary }: { items: AgendaItem[]; summary: OrderSummary }) {
  const firstLater = items.findIndex((x) => x.bucket === 'not_yet')
  return (
    <List dense disablePadding>
      {items.map((x, i) => [
        (i === 0 || items[i - 1].group !== x.group) &&
          groupHeading(x.group ?? '', items[i - 1]?.group ?? '', summary) && (
            <ListSubheader key={`group-${x.group}`} disableGutters sx={{ lineHeight: 2.5 }}>
              {groupHeading(x.group ?? '', items[i - 1]?.group ?? '', summary)}
            </ListSubheader>
          ),
        i === firstLater && (
          <ListSubheader key="not-yet" disableGutters sx={{ lineHeight: 2.5 }}>
            Not yet startable
          </ListSubheader>
        ),
        // Every row here needs you: a meeting row opens its brief.
        <AgendaRow key={x.id} x={x} needsYou>
          {(actions) => (
            <ListItem disableGutters sx={CHIP_ROW}>
              <ListItemText
                primary={<AgendaLink x={x} />}
                secondary={x.why || undefined}
                slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
              />
              {x.kind && (
                <StatusChip tone={x.kind === 'incident' ? 'bad' : 'quiet'} label={x.kind} />
              )}
              <ReadyChip x={x} />
              <AgentLooking id={x.id} />
              <AgentWorking id={x.id} />
              {x.due && <DueChip due={x.due} />}
              {actions}
            </ListItem>
          )}
        </AgendaRow>,
      ])}
    </List>
  )
}

// leadCard is the one thing to look at first, with its actions.
function leadCard(lead: Lead, actions: ReactNode) {
  return (
    <Paper
      variant="outlined"
      sx={{ p: 1.5, mb: 2, borderLeft: 4, borderLeftColor: 'primary.main' }}
    >
      <Typography variant="overline" color="text.secondary">
        Start here - {lead.why}
      </Typography>
      {/* The title keeps at least 14em before the actions go under it. */}
      <Stack
        direction="row"
        useFlexGap
        sx={{ alignItems: 'center', flexWrap: 'wrap', columnGap: 1, rowGap: 0.5 }}
      >
        <Typography
          variant="h6"
          component="p"
          sx={{ flex: '1 1 14em', minWidth: 0, overflowWrap: 'anywhere' }}
        >
          {lead.title}
        </Typography>
        {lead.row && <ReadyChip x={lead.row} />}
        {actions}
      </Stack>
    </Paper>
  )
}

function Agenda() {
  const { by } = Route.useSearch()
  const q = useToday()
  const inbox = useIoList('in')
  const questions = useQuestions()
  const requests = useRequests()
  const narrow = useMediaQuery('(max-width: 599px)')
  const [more, setMore] = useState<string>('')
  return (
    <>
      <PageTitle
        action={
          <Stack direction="row" spacing={1}>
            <LinkButton size="small" to="/obligations">
              Obligations
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
            // Every list here is what needs you (needs_you on the server): nothing closed, put off,
            // or an agent's. An answer an older agent version proposed: shown, decided in the chat.
            const checks = (questions.data ?? []).filter((x) => x.hasProposal && x.needsYou)
            const agenda: AgendaItem[] = t.agenda.items
            const agentWork = (requests.data ?? []).filter(
              (x) => x.status === 'proposed' && x.needsYou,
            )
            const inboxItems = (inbox.data?.items ?? []).filter((x) => x.needsYou)
            // need is the one count of what needs you; the stack and Today count the same set.
            const need = t.needsYou

            // The lead is the one thing to look at first: a proposed answer, then the top
            // of the agenda, then the inbox. It is not listed again.
            let lead: Lead | null = null
            let leadKind: 'answers' | 'agenda' | 'inbox' | '' = ''
            const firstCheck = checks[0]
            const firstA = agenda[0]?.bucket === 'not_yet' ? undefined : agenda[0]
            const firstIn = inboxItems[0]
            if (firstCheck) {
              leadKind = 'answers'
              lead = {
                why: `${t.answersToCheck} proposed answer${t.answersToCheck === 1 ? '' : 's'}, from ${firstCheck.proposedBy || 'an agent'}`,
                title: <DocLink id={firstCheck.id}>{firstCheck.title}</DocLink>,
                action: null,
              }
            } else if (firstA) {
              leadKind = 'agenda'
              lead = {
                why: firstA.why || firstA.kind || 'First in order',
                title: <AgendaLink x={firstA} />,
                action: null,
                row: firstA,
              }
            } else if (firstIn) {
              leadKind = 'inbox'
              lead = {
                why: 'In the inbox, not sorted',
                title: (
                  <TextLink to="/io/$id" params={{ id: firstIn.id }} underline="hover">
                    {firstIn.title || 'Untitled message'}
                  </TextLink>
                ),
                action: null,
              }
            }

            const checkRows = checks.slice(leadKind === 'answers' ? 1 : 0)
            const agendaRows = agenda.slice(leadKind === 'agenda' ? 1 : 0)
            const inboxRows = inboxItems.filter(
              (x) => !(leadKind === 'inbox' && x.id === firstIn?.id),
            )

            const inboxOf = (list: typeof inboxRows) => (
              <List dense disablePadding>
                {list.map((i) => (
                  <AgendaRow key={i.id} x={{ id: i.id, title: i.title, kind: 'io' }}>
                    {(actions) => (
                      <ListItem disableGutters sx={CHIP_ROW}>
                        <ListItemText
                          primary={
                            <TextLink to="/io/$id" params={{ id: i.id }} underline="hover">
                              {i.title || 'Untitled message'}
                            </TextLink>
                          }
                          secondary={humanTime(i.at || i.created)}
                          sx={{ overflowWrap: 'anywhere' }}
                        />
                        {actions}
                      </ListItem>
                    )}
                  </AgendaRow>
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
                body: <ItemRows kind="doc" items={t.blocked.items} rowKind="task" />,
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
                    File sync left {t.syncConflicts.length} conflict copies and{' '}
                    {t.lostUpdates.length} lost updates.
                  </Alert>
                )}
                {need === 0 && (
                  <Typography variant="body1" sx={{ mb: 1.5, fontWeight: 600 }}>
                    Nothing needs you right now.
                  </Typography>
                )}
                {lead &&
                  (lead.row ? (
                    <AgendaRow x={lead.row}>{(actions) => leadCard(lead, actions)}</AgendaRow>
                  ) : (
                    leadCard(lead, lead.action)
                  ))}
                {(rest.length > 0 ||
                  t.unverified > 0 ||
                  t.docsUnseen > 0 ||
                  t.docComments > 0 ||
                  t.agentOpen > 0) && (
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
                        <LinkButton size="small" color="inherit" to="/areas">
                          Summaries to verify {t.unverified}
                        </LinkButton>
                      )}
                      {t.agentOpen > 0 && (
                        <LinkButton
                          size="small"
                          color="inherit"
                          to="/requests"
                          data-agent-open={t.agentOpen}
                        >
                          {openLine(t.agentOpen, t.agentStale)}
                        </LinkButton>
                      )}
                      {(t.docsUnseen > 0 || t.docComments > 0) && (
                        <LinkButton size="small" color="inherit" to="/docs">
                          Docs changed {t.docsUnseen}, open comments {t.docComments}
                        </LinkButton>
                      )}
                    </Stack>
                    {rest.find((r) => r.key === more)?.body}
                  </Box>
                )}
                <Section
                  title="Proposed answers (older agent version)"
                  count={t.answersToCheck}
                  hideEmpty
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
                  title="Agent work proposed"
                  count={agentWork.length}
                  hideEmpty
                  action={
                    <LinkButton size="small" to="/requests">
                      Asked of agents
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
                <PoolsDue />
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
                  <AgendaRows items={agendaRows} summary={t.orderSummary} />
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
                {(t.optional.count > 0 || t.skipped.count > 0) && (
                  <Box sx={{ mt: 1 }}>
                    <OptionalLine items={t.optional.items} />
                    <SkippedLine items={t.skipped.items} />
                  </Box>
                )}
                {t.health > 0 && (
                  <LinkButton size="small" color="inherit" to="/health">
                    {t.health} health finding{t.health === 1 ? '' : 's'}
                  </LinkButton>
                )}
                {t.upgradeSteps > 0 && (
                  <Typography variant="body2" color="text.secondary">
                    {t.upgradeSteps} vault upgrade step{t.upgradeSteps === 1 ? '' : 's'} pending.
                    Run para vault upgrade status.
                  </Typography>
                )}
                <Section title="Inbox to sort" count={inboxRows.length} hideEmpty>
                  {inboxOf(inboxRows)}
                </Section>
                {(t.inbox > 0 || t.outbox > 0) && (
                  <LinkButton size="small" color="inherit" to="/io">
                    {t.inbox} unread in the inbox, {t.outbox} ready to send
                  </LinkButton>
                )}
                {need === 0 && <Empty>No task is ready.</Empty>}
              </>
            )
          }}
        </DataState>
      )}
    </>
  )
}
