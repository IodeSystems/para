import { createFileRoute } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { usePinned, type PinnedCard } from '@/api/pin'
import { Empty, PageTitle, StateChip, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { LinkButton, TextLink } from '@/components/links'
import { PinButton } from '@/components/PinButton'
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
import { exactTime } from '@/lib/when'

export const Route = createFileRoute('/_authed/pinned')({ component: Pinned })

// Pinned is the home of the owner: the items they watch (goals, threads, open questions, Asks,
// obligations, and the rest), as cards. The server orders them: needs you first, then the next
// date, then the newest pin. Next is one click away.
function Pinned() {
  const q = usePinned()
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
