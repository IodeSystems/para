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
import { useOrder } from '@/api/next'
import { useQuestion, useQuestionReview } from '@/api/questions'
import { canDecide } from '@/client/session'
import { errorText } from '@/client/gqlClient'
import { ItemActions } from '@/components/AgendaActions'
import { SourceList } from '@/components/AnswerCheck'
import { PageTitle, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DoItTask } from '@/components/DoIt'
import { DraftReady, cardBox } from '@/components/output/ProposalCard'
import { AgentLooking } from '@/components/FindContext'
import { Thread } from '@/components/Thread'
import { ItemComposer } from '@/components/ItemComposer'
import { ItemDates } from '@/components/ItemDates'
import { ItemDraftProvider } from '@/components/ItemDraft'
import { useNextKeys } from '@/components/Keyboard'
import { LinkButton } from '@/components/links'
import { Markdown } from '@/components/Markdown'
import { BriefBody } from '@/components/MeetingBrief'
import { CopyId } from '@/components/CopyId'
import { PinButton } from '@/components/PinButton'
import { PoolCard } from '@/components/Pools'
import { ProposedClose } from '@/components/ProposedClose'
import { KPIReadingEditor } from '@/components/ProjectBits'
import { DueChip } from '@/components/TaskRows'
import { PartOfLine } from '@/components/PartOf'
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
  isQuickBatch,
  left,
  skipCard,
  withQuickBatch,
} from '@/lib/next'
import { RetryFix } from '@/components/RetryFix'
import { nameOr } from '@/lib/names'
import { isKPIRow, kpiRowParts } from '@/lib/kpi'
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
  // batchOpen: the owner opened the quick approvals card; its rows are cards in its place.
  const [batchOpen, setBatchOpen] = useState(false)
  const deck = deckOf(d.rows)
  const rest = left(deck, gone)
  const card = currentCard(withQuickBatch(deck, gone, batchOpen), skipped, gone)

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
          {isQuickBatch(card) ? (
            <QuickBatchNext
              batch={card.batch}
              canBack={skipped.length > 0}
              onOpen={() => setBatchOpen(true)}
              onSkip={() => skip(card.id)}
              onBack={() => setSkipped(backCard)}
            />
          ) : (
            <Card
              x={card}
              decide={decide}
              canBack={skipped.length > 0}
              onGone={(g) => (g ? goneOut(card.id) : back(card.id))}
              onSkip={() => skip(card.id)}
              onBack={() => setSkipped(backCard)}
            />
          )}
        </Box>
      ) : (
        <Empty skipped={rest.length} onAgain={() => setSkipped([])} />
      )}
    </>
  )
}

// QuickBatchNext is the card of the quick approvals batch: ready approvals (a draft, a proposed
// answer, a proposed close) that unblock nothing, after the focus cards. Open walks them one by one;
// Skip passes the whole batch.
function QuickBatchNext({
  batch,
  canBack,
  onOpen,
  onSkip,
  onBack,
}: {
  batch: Row[]
  canBack: boolean
  onOpen: () => void
  onSkip: () => void
  onBack: () => void
}) {
  const shown = batch.slice(0, 5)
  return (
    <Paper
      variant="outlined"
      data-next-card="quick:batch"
      data-next-kind="quick"
      sx={{ p: { xs: 1.5, sm: 2 }, borderLeft: 4, borderLeftColor: 'success.main' }}
    >
      <Typography variant="overline" color="text.secondary" component="p">
        Quick approvals - each is ready and unblocks nothing
      </Typography>
      <Typography variant="h5" component="h2">
        Quick approvals ({batch.length})
      </Typography>
      <Box component="ul" sx={{ my: 1, pl: 2.5 }}>
        {shown.map((r) => (
          <li key={r.id}>
            <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
              {r.title || nameOr('', r.id)}
              {r.readyKind ? ` (${readyWords(r.readyKind).replace(/^Ready: /, '')})` : ''}
            </Typography>
          </li>
        ))}
        {batch.length > shown.length && (
          <li>
            <Typography variant="body2" color="text.secondary">
              and {batch.length - shown.length} more
            </Typography>
          </li>
        )}
      </Box>
      <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap' }}>
        <Button size="small" variant="contained" data-act="approve" onClick={onOpen}>
          Open, one by one
        </Button>
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

// focusComposer puts the cursor in the card's composer (the send key).
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
        <LinkButton size="small" to="/input" search={{ tab: 'rest' }}>
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
  if (isKPIRow(x))
    return <KPINext x={x} canBack={canBack} onGone={onGone} onSkip={onSkip} onBack={onBack} />
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
        sx={{ p: { xs: 1.5, sm: 2 }, borderLeft: 4, borderLeftColor: 'primary.main' }}
      >
        {/* On a phone the reason line takes the full width and the id sits under it, so the
            header is two short lines, not a tall column beside the id. */}
        <Stack
          direction={{ xs: 'column', sm: 'row' }}
          spacing={0.5}
          sx={{
            alignItems: { xs: 'flex-start', sm: 'center' },
            justifyContent: 'space-between',
          }}
        >
          <Typography variant="overline" color="text.secondary" component="p">
            {[group, x.why].filter(Boolean).join(' - ')}
          </Typography>
          <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center', maxWidth: '100%' }}>
            <CopyId id={x.id} />
            <PinButton id={x.id} />
          </Stack>
        </Stack>
        <Typography variant="h5" component="h2" sx={{ overflowWrap: 'anywhere' }}>
          {title}
        </Typography>
        {x.reason && (
          <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
            {x.reason}
          </Typography>
        )}
        {(x.partOf?.length ?? 0) > 0 && (
          <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
            <PartOfLine parts={x.partOf} />
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
          </ItemActions>
        </Stack>
        <Box sx={{ my: 1 }}>
          <Thread id={x.id} fill={false} maxHeight={280} hideEmpty />
        </Box>
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
      sx={{ p: { xs: 1.5, sm: 2 }, borderLeft: 4, borderLeftColor: 'primary.main' }}
    >
      <Stack
        direction="row"
        spacing={1}
        sx={{ alignItems: 'center', justifyContent: 'space-between' }}
      >
        <Typography variant="overline" color="text.secondary" component="p">
          {[groupWords(x.group), x.why].filter(Boolean).join(' - ')}
        </Typography>
        <CopyId id={x.id} />
      </Stack>
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
  return (
    <ItemComposer
      id={id}
      title={title}
      meta={doc.data.meta}
      onSent={onSent}
      onBack={onBack}
      stickyOnPhone={false}
    />
  )
}

// Prepared shows what an agent prepared, inline: a draft reply with Approve and Edit, a proposed
// answer with Accept and Edit, a proposed close with Approve close, Edit, and Not it, or the plan
// or fields filled in on the item.
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
  if (x.readyKind === 'close')
    return <ProposedClose id={x.id} title={x.title || x.id} decide={decide} onGone={onGone} />
  // A brief on a meeting is the brief the card shows.
  if (x.readyKind === 'brief' && x.kind === 'meeting') return null
  return <FilledReady id={target} />
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
    <Box sx={cardBox} data-next-answer={id}>
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
        <LinkButton size="small" data-act="edit" to="/docs/$id" params={{ id }}>
          Edit
        </LinkButton>
      </Stack>
    </Box>
  )
}

// FilledReady is a plan or fields an agent filled in on the item (or the document it names): its
// text, and Edit on its page. Check it, then Done or send words to an agent.
function FilledReady({ id }: { id: string }) {
  const q = useDoc(id)
  if (q.isError) return <Alert severity="warning">{errorText(q.error)}</Alert>
  return (
    <Box sx={{ ...cardBox, maxHeight: 360, overflowY: 'auto' }} data-next-filled={id}>
      {q.data?.body?.trim() ? (
        <Markdown source={q.data.body} />
      ) : (
        <Typography variant="body2" color="text.secondary">
          Filled in on the item: open it to check.
        </Typography>
      )}
      <LinkButton size="small" data-act="edit" to="/docs/$id" params={{ id }}>
        Edit
      </LinkButton>
    </Box>
  )
}

// KPINext is the card of a KPI the owner owns that is off target or stale: one card per KPI, made
// from the project, so a reading on target takes it away. It is not a document: no composer and no
// item actions. The owner adds a reading here or opens the project.
function KPINext({
  x,
  canBack,
  onGone,
  onSkip,
  onBack,
}: {
  x: Row
  canBack: boolean
  onGone: (gone: boolean) => void
  onSkip: () => void
  onBack: () => void
}) {
  const parts = kpiRowParts(x.id)
  return (
    <Paper
      variant="outlined"
      data-next-card={x.id}
      data-next-kind={x.kind}
      sx={{ p: { xs: 1.5, sm: 2 }, borderLeft: 4, borderLeftColor: 'warning.main' }}
    >
      <Typography variant="overline" color="text.secondary" component="p">
        {[groupWords(x.group), x.why].filter(Boolean).join(' - ')}
      </Typography>
      <Typography variant="h5" component="h2" sx={{ overflowWrap: 'anywhere' }}>
        {x.title}
      </Typography>
      {x.reason && (
        <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
          {x.reason}
        </Typography>
      )}
      {parts && (
        <Stack spacing={1} sx={{ my: 1 }}>
          <KPIReadingEditor id={parts.project} slug={parts.slug} onSaved={() => onGone(true)} />
        </Stack>
      )}
      <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', mt: 1 }}>
        {parts && (
          <LinkButton size="small" to="/projects/$id" params={{ id: parts.project }}>
            Open project
          </LinkButton>
        )}
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
