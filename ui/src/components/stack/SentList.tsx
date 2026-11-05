import { useQueryClient } from '@tanstack/react-query'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useSent } from '@/api/proposals'
import { useRequests } from '@/api/requests'
import { useSourceList } from '@/api/sources'
import { agentUnsend } from '@/api/composer'
import { errorText } from '@/client/gqlClient'
import { Empty, Section, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { LinkButton, TextLink } from '@/components/links'
import { useToasts } from '@/components/ToastStack'
import { humanTime } from '@/lib/when'
import { kindWords, sentStatus, staleWords, waiting } from '@/lib/sent'
import { stripIds, withNames } from '@/lib/names'

// SentList is the Asked of agents view. First every piece of agent work the owner asked for,
// newest first: Send to agent from the web, and what the owner asked an agent in the chat
// (owner-ask). Each shows the item, the owner's words, where it stands, how it ended, and the
// receipts it made; work no agent has claimed can be taken back. Then the queue and the sweeps:
// what waits for an agent, oldest first, and the last sweep of each source.
export function SentList({ decide }: { decide: boolean }) {
  const q = useSent()
  const qc = useQueryClient()
  const toasts = useToasts()
  const takeBack = async (request: string, name: string) => {
    try {
      await agentUnsend(request)
      toasts.push({ text: `Taken back: ${name}` })
    } catch (e) {
      toasts.push({ text: `Could not take it back: ${errorText(e)}`, tone: 'error' })
    }
    await qc.invalidateQueries()
  }
  return (
    <>
      <DataState q={q}>
        {(d) =>
          d.items.length === 0 ? (
            <Empty>
              Nothing asked of an agent yet. What you send to an agent here, and what you ask an
              agent in the chat that does not end in its reply, shows here.
            </Empty>
          ) : (
            <>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                {d.open} open{d.stale > 0 ? `, ${d.stale} stale` : ''}
              </Typography>
              <List aria-label="Asked of agents" disablePadding>
                {d.items.map((it) => {
                  const s = sentStatus(it)
                  const stale = staleWords(it)
                  return (
                    <ListItem
                      key={it.request}
                      divider
                      disableGutters
                      data-sent={it.request}
                      data-kind={it.kind}
                      sx={{ display: 'block' }}
                    >
                      <Stack
                        direction="row"
                        spacing={1}
                        sx={{ alignItems: 'center', flexWrap: 'wrap' }}
                      >
                        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
                          <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
                            {it.item ? (
                              <DocLink id={it.item}>{it.itemName || it.item}</DocLink>
                            ) : (
                              it.title
                            )}
                          </Typography>
                          {it.item && it.kind === 'owner-ask' && (
                            <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
                              {it.title}
                            </Typography>
                          )}
                          {it.note && (
                            <Typography
                              variant="body2"
                              color="text.secondary"
                              sx={{ overflowWrap: 'anywhere' }}
                            >
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
                                receipts:
                              </Typography>
                            )}
                            {(it.receipts ?? []).map((r) => (
                              <Typography key={r} variant="caption" data-receipt={r}>
                                <DocLink id={r}>{r}</DocLink>
                              </Typography>
                            ))}
                          </Stack>
                        </Box>
                        {decide && it.undoable && (
                          <Button
                            size="small"
                            onClick={() => void takeBack(it.request, it.itemName || it.title)}
                          >
                            Take back
                          </Button>
                        )}
                      </Stack>
                    </ListItem>
                  )
                })}
              </List>
            </>
          )
        }
      </DataState>
      <QueueAndSweeps />
    </>
  )
}

// WaitingOnYou is agent work that ended stuck, blocked, or needs clarification: it needs the
// owner, so the needs-you view of the stack lists it above the items. The owner answers in the
// chat, or with para request respond.
export function WaitingOnYou({
  items,
}: {
  items: {
    request: string
    title: string
    item?: string | null
    itemName?: string | null
    note?: string | null
    status: string
    resolution?: string | null
    summary?: string | null
    queuedAt: string
  }[]
}) {
  if (items.length === 0) return null
  return (
    <Section title="Agent work waiting on you" count={items.length}>
      <List dense disablePadding aria-label="Agent work waiting on you">
        {items.map((it) => {
          const s = sentStatus(it)
          return (
            <ListItem key={it.request} disableGutters data-waits={it.request}>
              <ListItemText
                primary={
                  <>
                    <DocLink id={it.request}>{it.title || 'Agent work'}</DocLink>
                    {it.item && (
                      <>
                        {' about '}
                        <DocLink id={it.item}>{it.itemName || it.item}</DocLink>
                      </>
                    )}
                  </>
                }
                secondary={[
                  it.summary && `Agent: ${it.summary}`,
                  it.note && `You asked: ${it.note}`,
                  `Answer in the chat, or: para request respond ${it.request} clarify "<answer>"`,
                ]
                  .filter(Boolean)
                  .join(' - ')}
                sx={{ overflowWrap: 'anywhere' }}
              />
              <StatusChip tone={s.tone} label={s.label} />
            </ListItem>
          )
        })}
      </List>
    </Section>
  )
}

// QueueAndSweeps is what the loop has not finished: agent work queued or held by an agent,
// oldest first, and the last sweep of each source (from sweep_record).
function QueueAndSweeps() {
  const requests = useRequests()
  const sources = useSourceList()
  const queue = waiting(requests.data ?? [])
  const swept = sources.data?.sources ?? []
  return (
    <Section
      title="Queue and sweeps"
      count={queue.length}
      action={
        <Stack direction="row" spacing={1}>
          <LinkButton size="small" to="/requests">
            Agent work
          </LinkButton>
          <LinkButton size="small" to="/sources">
            Sources
          </LinkButton>
        </Stack>
      }
    >
      {queue.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          Nothing waits for an agent. Run /para loop in an agent to work the queue.
        </Typography>
      ) : (
        <List dense disablePadding aria-label="Queue">
          {queue.map((r) => {
            const s = sentStatus(r)
            const stale = staleWords(r)
            return (
              <ListItem key={r.id} disableGutters data-queued={r.id}>
                <ListItemText
                  primary={
                    <DocLink id={r.id}>
                      {stripIds(withNames(r.title, r.links, r.linkNames)) || 'Agent work'}
                    </DocLink>
                  }
                  secondary={[
                    r.requiresSource ? 'sweep' : r.kind || 'agent work',
                    `queued ${humanTime(r.queuedAt)}`,
                    s.label,
                    stale,
                  ]
                    .filter(Boolean)
                    .join(' - ')}
                  sx={{ overflowWrap: 'anywhere' }}
                />
              </ListItem>
            )
          })}
        </List>
      )}
      {swept.length > 0 && (
        <List dense disablePadding aria-label="Last sweeps">
          {swept.map((src) => (
            <ListItem key={src.id} disableGutters data-swept={src.id}>
              <ListItemText
                primary={
                  <TextLink to="/sources/$id" params={{ id: src.id }} underline="hover">
                    {src.title || src.id}
                  </TextLink>
                }
                secondary={src.lastSweep ? `last sweep ${src.lastSweep}` : 'never swept'}
                sx={{ overflowWrap: 'anywhere' }}
              />
              {src.lastFailed && <StatusChip tone="bad" label="Last sweep failed" />}
              {src.overdue && <StatusChip tone="attn" label="Overdue" />}
            </ListItem>
          ))}
        </List>
      )}
    </Section>
  )
}
