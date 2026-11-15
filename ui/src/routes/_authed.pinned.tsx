import { useState } from 'react'
import { createFileRoute, useNavigate, useRouteContext } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import FormControlLabel from '@mui/material/FormControlLabel'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useConversations, type ConversationRow } from '@/api/conversations'
import { useConverse } from '@/api/context'
import { usePinned, type PinnedCard } from '@/api/pin'
import { Empty, PageTitle, StateChip, StatusChip } from '@/components/Bits'
import { errorText } from '@/client/gqlClient'
import { canDecide } from '@/client/session'
import { DataState } from '@/components/DataState'
import { LinkButton, TextLink } from '@/components/links'
import { PinButton } from '@/components/PinButton'
import { useToasts } from '@/components/ToastStack'
import { activityTarget } from '@/lib/activity'
import {
  lastWords,
  lotsOfPlates,
  nextWords,
  PLATES,
  tagOf,
  threadWords,
  waitingWords,
} from '@/lib/pinned'
import { tellToast } from '@/lib/tell'
import { statusText } from '@/lib/thread'
import { withdrawTurn } from '@/api/thread'
import { requestEditTurn } from '@/lib/threadEdit'
import { exactTime, humanTime } from '@/lib/when'

export const Route = createFileRoute('/_authed/pinned')({ component: Pinned })

// Pinned is the home of the owner: the items they watch (goals, threads, open questions, Asks,
// obligations, and the rest), as cards. The server orders them: needs you first, then the next
// date, then the newest pin. Next is one click away.
function Pinned() {
  const q = usePinned()
  const { session } = useRouteContext({ from: '/_authed' })
  return (
    <>
      <PageTitle
        subtitle="What you watch. Pin an item with the pin on its page or its card."
        action={
          <LinkButton to="/next" size="small" variant="outlined" data-link="next">
            Next
          </LinkButton>
        }
      >
        Pinned
      </PageTitle>
      {canDecide(session) && <HomeComposer />}
      <Conversations />
      <DataState q={q}>
        {(cards) => {
          if (cards.length === 0) return <EmptyPins />
          const plates = lotsOfPlates(cards)
          return (
            <>
              {plates && (
                <Alert severity="info" sx={{ mb: 2 }} data-plates={plates.count}>
                  {`Lots of plates: ${plates.count} pinned (past ${PLATES}). `}
                  {plates.suggest.length > 0
                    ? 'These are closed or quiet for 30 days; unpin what you no longer watch.'
                    : 'Nothing is closed or quiet yet; unpin what you no longer watch.'}
                  <Stack component="ul" spacing={0.25} sx={{ m: 0, mt: 0.5, pl: 2 }}>
                    {plates.suggest.map((s) => (
                      <Stack
                        component="li"
                        key={s.id}
                        direction="row"
                        spacing={0.5}
                        sx={{ alignItems: 'center' }}
                      >
                        <span>
                          {s.title} ({s.why})
                        </span>
                        <PinButton id={s.id} />
                      </Stack>
                    ))}
                  </Stack>
                </Alert>
              )}
              <Box
                data-pinned-grid
                sx={{
                  display: 'grid',
                  gap: 1.5,
                  gridTemplateColumns: 'repeat(auto-fill, minmax(min(100%, 280px), 1fr))',
                }}
              >
                {cards.map((c) => (
                  <Card key={c.id} c={c} />
                ))}
              </Box>
            </>
          )
        }}
      </DataState>
    </>
  )
}

// HomeComposer: the owner says something to the agent from home. The words are saved as an io
// item and sent as a turn as written; "Research and file this" asks the agent to research it and
// file the result. Ctrl or Cmd+Enter sends.
function HomeComposer() {
  const [text, setText] = useState('')
  const [research, setResearch] = useState(false)
  const converse = useConverse()
  const toasts = useToasts()
  const navigate = useNavigate()
  const ready = text.trim() !== '' && !converse.isPending
  const send = () => {
    if (!ready) return
    converse.mutate(
      { text: text.trim(), research },
      {
        onSuccess: (out) => {
          setText('')
          setResearch(false)
          if (out.told) {
            const request = out.told.request
            toasts.push(
              tellToast(out.id, out.told, {
                undo: request ? async () => void (await withdrawTurn(request)) : undefined,
                edit: request
                  ? () => {
                      void navigate({ to: '/io/$id', params: { id: out.id } })
                      requestEditTurn(request)
                    }
                  : undefined,
              }),
            )
          } else
            toasts.push({
              text: `Saved, but your agent was not told: ${out.tellError ?? 'unknown error'}`,
              tone: 'error',
              link: { label: 'Open the item', io: out.id },
              target: { id: out.id, kind: 'io' },
              dwell: 15000,
            })
        },
      },
    )
  }
  return (
    <Paper variant="outlined" data-home-composer sx={{ p: 1.5, mb: 2 }}>
      <Stack spacing={1}>
        <TextField
          multiline
          minRows={2}
          maxRows={10}
          fullWidth
          label="Tell your agent"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
              e.preventDefault()
              send()
            }
          }}
        />
        {converse.isError && (
          <Alert severity="error" data-converse-error>
            {errorText(converse.error)}
          </Alert>
        )}
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center', justifyContent: 'flex-end' }}>
          <FormControlLabel
            control={
              <Checkbox
                size="small"
                checked={research}
                onChange={(e) => setResearch(e.target.checked)}
              />
            }
            label="Research and file this"
          />
          <Button variant="contained" size="small" disabled={!ready} onClick={send} data-converse-send>
            Send
          </Button>
        </Stack>
      </Stack>
    </Paper>
  )
}

// Conversations: threads with the agent that are open or have a reply not yet seen. The title
// opens the item, where the thread takes replies. Hidden when there are none.
function Conversations() {
  const q = useConversations()
  const rows = q.data ?? []
  if (rows.length === 0) return null
  return (
    <Box component="section" data-conversations sx={{ mb: 2 }}>
      <Typography variant="subtitle2" component="h2" color="text.secondary" sx={{ mb: 1 }}>
        Conversations
      </Typography>
      <Box
        sx={{
          display: 'grid',
          gap: 1.5,
          gridTemplateColumns: 'repeat(auto-fill, minmax(min(100%, 280px), 1fr))',
        }}
      >
        {rows.map((r) => (
          <ConversationCard key={r.id} r={r} />
        ))}
      </Box>
    </Box>
  )
}

function ConversationCard({ r }: { r: ConversationRow }) {
  return (
    <Paper
      variant="outlined"
      data-conversation={r.id}
      sx={{
        position: 'relative',
        p: 1.5,
        borderLeft: 4,
        borderLeftColor: r.waitingNoAgent ? 'warning.main' : 'divider',
        '&:hover': { bgcolor: 'action.hover' },
      }}
    >
      <Typography
        variant="subtitle1"
        component="h3"
        sx={{ overflowWrap: 'anywhere', lineHeight: 1.3 }}
      >
        <TextLink to="/io/$id" params={{ id: r.id }} underline="hover" sx={STRETCH}>
          {r.title || r.id}
        </TextLink>
      </Typography>
      <Typography variant="body2" color="text.secondary" component="p" sx={{ mt: 0.5 }}>
        {statusText({ kind: r.lastKind ?? 'other', at: r.lastAt, status: r.lastStatus })}
        {r.waitingNoAgent ? ' · no agent running' : ''}
        {' · '}
        <span title={exactTime(r.lastAt)}>{humanTime(r.lastAt)}</span>
      </Typography>
      {r.lastReply && (
        <Typography
          variant="body2"
          component="p"
          sx={{
            mt: 0.5,
            overflowWrap: 'anywhere',
            display: '-webkit-box',
            WebkitLineClamp: 2,
            WebkitBoxOrient: 'vertical',
            overflow: 'hidden',
          }}
        >
          {r.lastReply}
        </Typography>
      )}
    </Paper>
  )
}

function EmptyPins() {
  return (
    <Empty>
      Nothing is pinned. Pin what you want to watch: a goal, a thread, an open question, an Ask, or
      an obligation. Use the pin next to the title on its page, or on its card on Next. A pinned
      item shows here with its next date, who it waits on, and its last change.
    </Empty>
  )
}

// Card is one pinned item. The title is the link to the item; the pin is above it, so a click on
// the pin does not open the item.
function Card({ c }: { c: PinnedCard }) {
  const tag = tagOf(c.key)
  const target = activityTarget({ doc: c.id, docType: c.type })
  const next = nextWords(c)
  const waiting = waitingWords(c)
  const last = lastWords(c)
  const thread = threadWords(c)
  const title = c.title || c.id
  const link =
    c.type === 'thread' && tag ? (
      <TextLink to="/tags/$tag" params={{ tag }} underline="hover" sx={STRETCH}>
        {title}
      </TextLink>
    ) : (
      <TextLink to={target.to} params={{ id: target.id }} underline="hover" sx={STRETCH}>
        {title}
      </TextLink>
    )
  return (
    <Paper
      variant="outlined"
      data-pinned-card={c.id}
      data-needs-you={c.needsYou ? 'yes' : undefined}
      sx={{
        position: 'relative',
        p: 1.5,
        borderLeft: 4,
        borderLeftColor: c.needsYou ? 'warning.main' : 'divider',
        '&:hover': { bgcolor: 'action.hover' },
      }}
    >
      <Stack direction="row" spacing={0.5} sx={{ alignItems: 'flex-start' }}>
        <Typography
          variant="subtitle1"
          component="h2"
          sx={{ flexGrow: 1, overflowWrap: 'anywhere', lineHeight: 1.3 }}
        >
          {link}
        </Typography>
        <Box sx={{ position: 'relative', zIndex: 1 }}>
          <PinButton
            id={c.type === 'thread' ? undefined : c.id}
            pinKey={c.type === 'thread' ? (c.key ?? undefined) : undefined}
          />
        </Box>
      </Stack>
      <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', my: 0.75 }}>
        <StatusChip tone="quiet" label={c.type} />
        {c.state && <StateChip state={c.state} />}
        {c.needsYou && <StatusChip tone="attn" label="Needs you" />}
        {next && <StatusChip tone="quiet" label={next} />}
        {thread && <StatusChip tone={c.claimed + c.queued > 0 ? 'good' : 'quiet'} label={thread} />}
      </Stack>
      {c.mattersName || c.matters ? (
        <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
          For {c.mattersName || c.matters}
        </Typography>
      ) : null}
      {c.needsYou && c.reason && (
        <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
          {c.reason}
        </Typography>
      )}
      {waiting && (
        <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
          {waiting}
        </Typography>
      )}
      {c.lastReply && (
        <Typography
          variant="body2"
          color="text.secondary"
          sx={{ overflowWrap: 'anywhere', fontStyle: 'italic' }}
        >
          Last reply: {c.lastReply}
        </Typography>
      )}
      {last && (
        <Typography
          variant="caption"
          color="text.secondary"
          component="p"
          title={c.last ? exactTime(c.last.at) : undefined}
          sx={{ mt: 0.5 }}
        >
          {last}
        </Typography>
      )}
    </Paper>
  )
}

// STRETCH makes the title link cover the whole card.
const STRETCH = {
  color: 'text.primary',
  '&::after': { content: '""', position: 'absolute', inset: 0 },
}
