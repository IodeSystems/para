import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { requestSeen, useAsked, useRequestChanges } from '@/api/requests'
import { agentUnsend } from '@/api/composer'
import { Agents } from '@/components/Agents'
import { errorText } from '@/client/gqlClient'
import { canDecide } from '@/client/session'
import { Empty, PageTitle, Section, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { LinkButton } from '@/components/links'
import { RetryFix } from '@/components/RetryFix'
import { useToasts } from '@/components/ToastStack'
import { humanTime } from '@/lib/when'
import { byStatus, isFillGap, kindWords, resultToCheck, sentStatus, staleWords } from '@/lib/sent'

// Asked of agents is every piece of agent work, one request each, from the generic list (type
// request, state all), grouped by status: what the owner sent from the web, asked in the chat,
// delegated, or gave to investigate, and the work agents and the tick made; fill-gap work the tick
// made has its own group, Filled in by agents, and is not in the open count. Each row shows the
// item, the owner's words, where it stands, how it ended, and the receipts it made. Work no agent
// has claimed can be taken back; results not yet seen can be marked seen.
// Agents, above the groups, shows who is connected over MCP on this machine and what each holds.
export const Route = createFileRoute('/_authed/requests')({ component: Asked })

// Changes lists what finished agent work changed, from its receipts. It loads when opened.
function Changes({ id }: { id: string }) {
  const [open, setOpen] = useState(false)
  const changes = useRequestChanges(id, open)
  return (
    <Box
      component="details"
      onToggle={(e) => setOpen(e.currentTarget.open)}
      sx={{ fontSize: '0.8125rem' }}
    >
      <Box component="summary" sx={{ cursor: 'pointer' }}>
        What it changed
      </Box>
      <Stack spacing={0.5}>
        {(changes.data ?? []).map((c) => (
          <DocLink key={c.receipt} id={c.doc}>
            {c.docName || c.doc}
          </DocLink>
        ))}
        {changes.data?.length === 0 && 'Nothing.'}
      </Stack>
    </Box>
  )
}

type Row = NonNullable<ReturnType<typeof useAsked>['data']>[number]
type Work = NonNullable<Row['request']> & { needsYou: boolean; reason: string }

function Asked() {
  const q = useAsked()
  const qc = useQueryClient()
  const toasts = useToasts()
  const { session } = Route.useRouteContext()
  const decide = canDecide(session)
  const takeBack = async (request: string, name: string) => {
    try {
      await agentUnsend(request)
      toasts.push({ text: `Taken back: ${name}`, target: { id: request } })
    } catch (e) {
      toasts.push({ text: `Could not take it back: ${errorText(e)}`, tone: 'error' })
    }
    await qc.invalidateQueries()
  }
  const markSeen = async (ids: string[]) => {
    try {
      const r = await requestSeen(ids)
      toasts.push({ text: `Marked seen: ${r.seen.length}` })
    } catch (e) {
      toasts.push({ text: `Could not mark seen: ${errorText(e)}`, tone: 'error' })
    }
    await qc.invalidateQueries()
  }
  return (
    <>
      <PageTitle subtitle="Everything asked of agents, one record each, by status: what you sent from the web, asked in the chat, delegated, or gave to investigate, and the work agents and sweeps made. Filled in by agents lists the gaps agents fill on their own (a missing done-when, how to do it, effort, an answer to research, a goal brief). Not the same as Asks, which are asks to people.">
        Asked of agents
      </PageTitle>
      <Agents />
      <DataState q={q}>
        {(rows) => {
          const work: Work[] = rows.flatMap((r) =>
            r.request ? [{ ...r.request, needsYou: r.needsYou, reason: r.reason }] : [],
          )
          if (work.length === 0)
            return (
              <Empty>
                Nothing asked of an agent yet. What you send to an agent, and what you ask an agent
                in the chat that does not end in its reply, shows here.
              </Empty>
            )
          // Fill-gap work is not what the owner asked: the open count leaves it out.
          const open = work.filter(
            (w) => !isFillGap(w) && ['proposed', 'queued', 'claimed'].includes(w.status),
          )
          const stale = open.filter((w) => w.stale).length
          const unseen = work.filter(resultToCheck).map((w) => w.request)
          return (
            <>
              <Stack
                direction="row"
                spacing={1}
                useFlexGap
                sx={{ mb: 1, alignItems: 'center', flexWrap: 'wrap' }}
              >
                <Typography variant="body2" color="text.secondary" data-asked-open={open.length}>
                  {open.length} open{stale > 0 ? `, ${stale} stale` : ''}
                  {unseen.length > 0 ? `, ${unseen.length} results to check` : ''}
                </Typography>
                {decide && unseen.length > 0 && (
                  <Button size="small" data-mark-seen onClick={() => void markSeen(unseen)}>
                    Mark seen ({unseen.length})
                  </Button>
                )}
                <LinkButton size="small" to="/sources">
                  Sources
                </LinkButton>
              </Stack>
              {byStatus(work).map((g) => (
                <Section key={g.status} title={g.title} count={g.rows.length}>
                  <List aria-label={g.title} disablePadding data-status-group={g.status}>
                    {g.rows.map((it) => (
                      <WorkRow
                        key={it.request}
                        it={it}
                        decide={decide}
                        takeBack={takeBack}
                        markSeen={markSeen}
                      />
                    ))}
                  </List>
                </Section>
              ))}
            </>
          )
        }}
      </DataState>
    </>
  )
}

function WorkRow({
  it,
  decide,
  takeBack,
  markSeen,
}: {
  it: Work
  decide: boolean
  takeBack: (request: string, name: string) => Promise<void>
  markSeen: (ids: string[]) => Promise<void>
}) {
  const s = sentStatus(it)
  const stale = staleWords(it)
  const check = resultToCheck(it)
  return (
    <ListItem
      divider
      disableGutters
      data-sent={it.request}
      data-kind={it.kind}
      data-seen={it.seen ? 'yes' : 'no'}
      sx={{ display: 'block' }}
    >
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
            {it.item ? <DocLink id={it.item}>{it.itemName || it.item}</DocLink> : it.title}
          </Typography>
          {it.item && it.kind === 'owner-ask' && (
            <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
              {it.title}
            </Typography>
          )}
          {it.note && (
            <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
              You asked: {it.note}
            </Typography>
          )}
          {it.summary && (
            <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
              Agent: {it.summary}
            </Typography>
          )}
          <Stack
            direction="row"
            spacing={1}
            useFlexGap
            sx={{ mt: 0.25, flexWrap: 'wrap', alignItems: 'center' }}
          >
            <StatusChip tone={s.tone} label={s.label} />
            {stale && <StatusChip tone="attn" label={stale} />}
            {it.needsYou && <StatusChip tone="attn" label={it.reason || 'needs you'} />}
            <Typography variant="caption" color="text.secondary">
              {kindWords(it.kind)}, {humanTime(it.queuedAt)}
            </Typography>
            <Typography variant="caption">
              <DocLink id={it.request}>the request</DocLink>
            </Typography>
            {(it.results ?? []).map((r) => (
              <Typography key={r} variant="caption">
                <DocLink id={r}>{r}</DocLink>
              </Typography>
            ))}
            {(it.receipts ?? []).length > 0 && (
              <Typography variant="caption" color="text.secondary">
                log entries:
              </Typography>
            )}
            {(it.receipts ?? []).map((r) => (
              <Typography key={r} variant="caption" data-receipt={r}>
                <DocLink id={r}>{r}</DocLink>
              </Typography>
            ))}
          </Stack>
          {it.kind === 'retry-write' && it.needsYou && <RetryFix id={it.request} decide={decide} />}
          {(it.status === 'done' || it.status === 'failed') && <Changes id={it.request} />}
        </Box>
        {decide && it.undoable && (
          <Button size="small" onClick={() => void takeBack(it.request, it.itemName || it.title)}>
            Take back
          </Button>
        )}
        {decide && check && (
          <Button size="small" onClick={() => void markSeen([it.request])}>
            Mark seen
          </Button>
        )}
      </Stack>
    </ListItem>
  )
}
