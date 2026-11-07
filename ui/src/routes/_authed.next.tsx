import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { keyframes } from '@mui/material/styles'
import { useDoc } from '@/api/doc'
import { useIOApprove, useIoItem } from '@/api/io'
import { useOrder } from '@/api/next'
import { useQuestion, useQuestionReview } from '@/api/questions'
import { canDecide } from '@/client/session'
import { errorText } from '@/client/gqlClient'
import { ItemActions } from '@/components/AgendaActions'
import { SourceList } from '@/components/AnswerCheck'
import { PageTitle, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DoItTask } from '@/components/DoIt'
import { AgentLooking } from '@/components/FindContext'
import { ItemComposer } from '@/components/ItemComposer'
import { ItemDates } from '@/components/ItemDates'
import { ItemDraftProvider } from '@/components/ItemDraft'
import { VoiceNote } from '@/components/Voice'
import { useNextKeys } from '@/components/Keyboard'
import { LinkButton } from '@/components/links'
import { Markdown } from '@/components/Markdown'
import { BriefBody } from '@/components/MeetingBrief'
import { PoolCard } from '@/components/Pools'
import { DueChip } from '@/components/TaskRows'
import { useToasts } from '@/components/ToastStack'
import { proposedText } from '@/lib/answer'
import type { NextKey } from '@/lib/keys'
import {
  backCard,
  currentCard,
  deckOf,
  effortWords,
  groupWords,
  headerLine,
  left,
  skipCard,
} from '@/lib/next'
import { RetryFix } from '@/components/RetryFix'
import { nameOr } from '@/lib/names'
import { isPoolRow } from '@/lib/pools'
import { readyTarget, readyWords } from '@/lib/ready'
import { prefersReducedMotion } from '@/lib/toasts'

export const Route = createFileRoute('/_authed/next')({ component: NextPage })

type Order = NonNullable<ReturnType<typeof useOrder>['data']>
type Row = Order['rows'][number]

// LEAVE_MS is how long a card takes to leave, as on the stack.
const LEAVE_MS = 180

const slideIn = keyframes`
  from { transform: translateX(24px); opacity: 0; }
  to { transform: none; opacity: 1; }
`

// NextPage is one card: the top of the value order that needs you. Act on it (Done, Later, Send to
// agent, Approve) or Skip; the card leaves and the next one comes. A skip is kept in memory only:
// a reload starts the deck again.
function NextPage() {
  const q = useOrder()
  return (
    <>
      <PageTitle subtitle="The one thing to do now, from the top of the value order. Agents prepare; you approve, do it, or skip.">
        Next
      </PageTitle>
      <DataState q={q}>{(d) => <Deck d={d} />}</DataState>
    </>
  )
}

function Deck({ d }: { d: Order }) {
  const { session } = Route.useRouteContext()
  const decide = canDecide(session)
  const [skipped, setSkipped] = useState<string[]>([])
  const [gone, setGone] = useState<ReadonlySet<string>>(() => new Set())
  const [leaving, setLeaving] = useState<string | null>(null)
  const deck = deckOf(d.rows)
  const rest = left(deck, gone)
  const card = currentCard(deck, skipped, gone)

  // out takes the card away: it fades and slides, then then() drops it from the deck.
  const out = (id: string, then: () => void) => {
    setLeaving(id)
    setTimeout(
      () => {
        then()
        setLeaving((l) => (l === id ? null : l))
      },
      prefersReducedMotion() ? 0 : LEAVE_MS,
    )
  }
  const goneOut = (id: string) => out(id, () => setGone((g) => new Set([...g, id])))
  const back = (id: string) =>
    setGone((g) => {
      const n = new Set(g)
      n.delete(id)
      return n
    })
  const skip = (id: string) => out(id, () => setSkipped((s) => skipCard(s, id)))

  useNextKeys((k: NextKey) => {
    if (k === 'back') {
      setSkipped(backCard)
      return
    }
    if (!card) return
    if (k === 'next') return skip(card.id)
    const root = document.querySelector(`[data-next-card="${CSS.escape(card.id)}"]`)
    if (k === 'send') return focusComposer(card.id)
    root?.querySelector<HTMLElement>(`[data-act="${k}"]:not([disabled])`)?.click()
  })

  return (
    <>
      <Typography variant="body2" sx={{ mb: 1.5 }} data-next-header>
        {headerLine(rest, d.summary)}
        {skipped.length > 0 && ` · ${skipped.length} skipped`}
      </Typography>
      {card ? (
        <Box
          key={card.id}
          sx={{
            animation: `${slideIn} 180ms ease-out`,
            transition: `opacity ${LEAVE_MS}ms ease, transform ${LEAVE_MS}ms ease`,
            opacity: leaving === card.id ? 0 : 1,
            transform: leaving === card.id ? 'translateX(-32px)' : 'none',
            pointerEvents: leaving === card.id ? 'none' : undefined,
            '@media (prefers-reduced-motion: reduce)': { animation: 'none', transition: 'none' },
          }}
        >
          <Card
            x={card}
            decide={decide}
            canBack={skipped.length > 0}
            onGone={(g) => (g ? goneOut(card.id) : back(card.id))}
            onSkip={() => skip(card.id)}
            onBack={() => setSkipped(backCard)}
          />
        </Box>
      ) : (
        <Empty skipped={rest.length} onAgain={() => setSkipped([])} />
      )}
    </>
  )
}

// focusComposer puts the cursor in the card's composer, for Send to agent.
function focusComposer(id: string) {
  const box = document.querySelector<HTMLElement>(
    `[data-next-card="${CSS.escape(id)}"] [data-item-composer] textarea:not([aria-hidden="true"])`,
  )
  box?.focus()
  box?.scrollIntoView({ block: 'nearest' })
}

function Empty({ skipped, onAgain }: { skipped: number; onAgain: () => void }) {
  return (
    <Paper variant="outlined" sx={{ p: 2 }} data-next-empty>
      <Typography variant="h6" component="p">
        {skipped > 0 ? `You skipped all ${skipped}` : 'Nothing needs you'}
      </Typography>
      <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap', mt: 1 }}>
        {skipped > 0 && (
          <Button size="small" variant="contained" onClick={onAgain} data-act="again">
            Start again
          </Button>
        )}
        <LinkButton size="small" to="/agenda">
          Agenda
        </LinkButton>
        <LinkButton size="small" to="/io" search={{ view: 'digest' }}>
          Inbox digest
        </LinkButton>
        <LinkButton size="small" to="/requests">
          Asked of agents
        </LinkButton>
      </Stack>
    </Paper>
  )
}

// Card is one item: why it is here, what an agent prepared (or how to do it), the meeting brief,
// the actions, and the item's composer.
function Card({
  x,
  decide,
  canBack,
  onGone,
  onSkip,
  onBack,
}: {
  x: Row
  decide: boolean
  canBack: boolean
  onGone: (gone: boolean) => void
  onSkip: () => void
  onBack: () => void
}) {
  const group = groupWords(x.group)
  const effort = effortWords(x.effort)
  const title = x.title || nameOr('', x.id)
  if (isPoolRow(x))
    return (
      <PoolNext
        x={x}
        decide={decide}
        canBack={canBack}
        onGone={onGone}
        onSkip={onSkip}
        onBack={onBack}
      />
    )
  return (
    <ItemDraftProvider id={x.id}>
      <Paper
        variant="outlined"
        data-next-card={x.id}
        data-next-kind={x.kind}
        data-next-ready={x.ready ? (x.readyKind ?? 'yes') : undefined}
        sx={{ p: 2, borderLeft: 4, borderLeftColor: 'primary.main' }}
      >
        <Typography variant="overline" color="text.secondary" component="p">
          {[group, x.why].filter(Boolean).join(' - ')}
        </Typography>
        <Typography variant="h5" component="h2" sx={{ overflowWrap: 'anywhere' }}>
          {title}
        </Typography>
        {x.reason && (
          <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
            {x.reason}
          </Typography>
        )}
        <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', my: 1 }}>
          <StatusChip tone={x.kind === 'incident' ? 'bad' : 'quiet'} label={x.kind} />
          {effort && <StatusChip tone="quiet" label={`effort ${effort}`} />}
          {x.due && <DueChip due={x.due} />}
          {x.ready && <StatusChip tone="good" label={readyWords(x.readyKind)} />}
          <AgentLooking id={x.id} />
          <ItemDates created={x.created} updated={x.updated} />
        </Stack>
        {x.ready && <Prepared x={x} decide={decide} onGone={onGone} />}
        {!x.ready && x.kind === 'task' && <DoItTask id={x.id} done={false} />}
        {x.kind === 'request' && <RetryFix id={x.id} decide={decide} />}
        {x.kind === 'meeting' && (
          <Box sx={{ pl: 2, my: 1, borderLeft: 2, borderColor: 'divider' }}>
            <BriefBody id={x.id} />
          </Box>
        )}
        <Stack
          direction="row"
          spacing={0.5}
          useFlexGap
          sx={{ flexWrap: 'wrap', alignItems: 'center', mt: 1 }}
        >
          <ItemActions x={{ id: x.id, title, kind: x.kind }} onGone={onGone}>
            <Button size="small" data-act="skip" onClick={onSkip}>
              Skip
            </Button>
            {canBack && (
              <Button size="small" data-act="back" onClick={onBack}>
                Back
              </Button>
            )}
            {decide && (
              <Button size="small" data-act="send" onClick={() => focusComposer(x.id)}>
                Send to agent
              </Button>
            )}
          </ItemActions>
        </Stack>
        <CardComposer
          id={x.id}
          title={title}
          onSent={() => onGone(true)}
          onBack={() => onGone(false)}
        />
      </Paper>
    </ItemDraftProvider>
  )
}

// PoolNext is the card of a firing idea pool: one card for the pool, its ideas as a checklist to
// pick from. It is not a document, so it has no composer and no item actions.
function PoolNext({
  x,
  decide,
  canBack,
  onGone,
  onSkip,
  onBack,
}: {
  x: Row
  decide: boolean
  canBack: boolean
  onGone: (gone: boolean) => void
  onSkip: () => void
  onBack: () => void
}) {
  return (
    <Paper
      variant="outlined"
      data-next-card={x.id}
      data-next-kind={x.kind}
      sx={{ p: 2, borderLeft: 4, borderLeftColor: 'primary.main' }}
    >
      <Typography variant="overline" color="text.secondary" component="p">
        {[groupWords(x.group), x.why].filter(Boolean).join(' - ')}
      </Typography>
      <Typography variant="h5" component="h2" sx={{ overflowWrap: 'anywhere' }}>
        {x.title}
      </Typography>
      {x.reason && (
        <Typography variant="body2" color="text.secondary">
          {x.reason}
        </Typography>
      )}
      <PoolCard row={x.id} decide={decide} onDone={() => onGone(true)} />
      <Stack direction="row" spacing={0.5} sx={{ mt: 1 }}>
        <Button size="small" data-act="skip" onClick={onSkip}>
          Skip
        </Button>
        {canBack && (
          <Button size="small" data-act="back" onClick={onBack}>
            Back
          </Button>
        )}
      </Stack>
    </Paper>
  )
}

// CardComposer is the item's composer: Send to agent with words and files, Save as notes.
function CardComposer({
  id,
  title,
  onSent,
  onBack,
}: {
  id: string
  title: string
  onSent: () => void
  onBack: () => void
}) {
  const doc = useDoc(id)
  if (!doc.data) return null
  return <ItemComposer id={id} title={title} meta={doc.data.meta} onSent={onSent} onBack={onBack} />
}

// Prepared shows what an agent prepared, inline: a draft reply with Approve and Edit, a proposed
// answer with Accept and Edit, or the plan or fields filled in on the item.
function Prepared({
  x,
  decide,
  onGone,
}: {
  x: Row
  decide: boolean
  onGone: (gone: boolean) => void
}) {
  const target = readyTarget(x)
  if (x.readyKind === 'draft') return <DraftReady id={target} decide={decide} onGone={onGone} />
  if (x.readyKind === 'answer') return <AnswerReady id={x.id} decide={decide} onGone={onGone} />
  // A brief on a meeting is the brief the card shows.
  if (x.readyKind === 'brief' && x.kind === 'meeting') return null
  return <FilledReady id={target} />
}

const box = {
  my: 1,
  p: 1.5,
  border: 1,
  borderColor: 'divider',
  borderRadius: 1,
  bgcolor: 'action.hover',
} as const

// DraftReady is an outgoing draft: its text, Approve (its exact text becomes ready to send), and
// Edit on its page.
function DraftReady({
  id,
  decide,
  onGone,
}: {
  id: string
  decide: boolean
  onGone: (gone: boolean) => void
}) {
  const q = useIoItem(id)
  const approve = useIOApprove()
  const toasts = useToasts()
  const it = q.data?.item
  const approved = it?.state === 'ready'
  const run = async () => {
    try {
      await approve.mutateAsync(id)
      onGone(true)
      toasts.push({
        text: `Approved: ${it?.subject || it?.title || id}`,
        note: 'An agent sends it. Edit the draft to take the approval back.',
      })
    } catch (e) {
      toasts.push({ text: `Could not approve: ${errorText(e)}`, tone: 'error' })
    }
  }
  return (
    <Box sx={box} data-next-draft={id}>
      <Typography variant="subtitle2" component="h3">
        Draft reply{it?.to?.length ? ` to ${it.to.join(', ')}` : ''}
      </Typography>
      {it?.subject && <Typography variant="body2">Subject: {it.subject}</Typography>}
      <Typography
        component="div"
        variant="body2"
        sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', my: 1 }}
      >
        {q.data?.doc.body ?? ''}
      </Typography>
      {it && <VoiceNote item={it} />}
      <Stack direction="row" spacing={1}>
        {decide && !approved && (
          <Button
            size="small"
            variant="contained"
            color="success"
            data-act="approve"
            disabled={approve.isPending || !q.data}
            onClick={() => void run()}
          >
            {it?.voice === 'for_me' ? 'Approve as you' : 'Approve'}
          </Button>
        )}
        {approved && (
          <Typography variant="body2" color="text.secondary">
            Approved. An agent sends it.
          </Typography>
        )}
        <LinkButton size="small" data-act="edit" to="/io/$id" params={{ id }}>
          Edit
        </LinkButton>
      </Stack>
    </Box>
  )
}

// AnswerReady is an agent's proposed answer to a question: the answer, why, its sources, Accept
// (the owner answers with it), and Edit on the question's page. An answer cannot be undone here.
function AnswerReady({
  id,
  decide,
  onGone,
}: {
  id: string
  decide: boolean
  onGone: (gone: boolean) => void
}) {
  const q = useQuestion(id)
  const review = useQuestionReview()
  const toasts = useToasts()
  const qn = q.data?.question
  const text = qn ? proposedText(qn) : ''
  const accept = async () => {
    if (!qn) return
    const choice = !qn.proposedAnswer && qn.proposedChoice ? qn.proposedChoice : ''
    try {
      await review.mutateAsync({
        id,
        decision: 'answer',
        choices: choice ? [choice] : undefined,
        ownWords: choice ? undefined : text,
        note: qn.proposedWhy || undefined,
      })
      onGone(true)
      toasts.push({ text: `Answered: ${qn.title}`, note: 'An answer cannot be undone here.' })
    } catch (e) {
      toasts.push({ text: `Could not answer: ${errorText(e)}`, tone: 'error' })
    }
  }
  return (
    <Box sx={box} data-next-answer={id}>
      <Typography variant="subtitle2" component="h3">
        Proposed answer{qn?.proposedBy ? `, from ${qn.proposedBy}` : ''}
      </Typography>
      <Typography sx={{ overflowWrap: 'anywhere', my: 0.5 }}>{text}</Typography>
      {qn?.proposedWhy && (
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          Reason: {qn.proposedWhy}
        </Typography>
      )}
      <SourceList sources={qn?.proposedSources} />
      <Stack direction="row" spacing={1} sx={{ mt: 1 }}>
        {decide && (
          <Button
            size="small"
            variant="contained"
            color="success"
            data-act="approve"
            disabled={review.isPending || !text}
            onClick={() => void accept()}
          >
            Accept
          </Button>
        )}
        <LinkButton size="small" data-act="edit" to="/doc/$id" params={{ id }}>
          Edit
        </LinkButton>
      </Stack>
    </Box>
  )
}

// FilledReady is a plan or fields an agent filled in on the item (or the document it names): its
// text, and Edit on its page. Check it, then Done or Send to agent.
function FilledReady({ id }: { id: string }) {
  const q = useDoc(id)
  if (q.isError) return <Alert severity="warning">{errorText(q.error)}</Alert>
  return (
    <Box sx={{ ...box, maxHeight: 360, overflowY: 'auto' }} data-next-filled={id}>
      {q.data?.body?.trim() ? (
        <Markdown source={q.data.body} />
      ) : (
        <Typography variant="body2" color="text.secondary">
          Filled in on the item: open it to check.
        </Typography>
      )}
      <LinkButton size="small" data-act="edit" to="/doc/$id" params={{ id }}>
        Edit
      </LinkButton>
    </Box>
  )
}
