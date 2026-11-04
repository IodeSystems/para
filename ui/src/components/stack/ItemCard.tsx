import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { Details, StateChip, StatusChip } from '@/components/Bits'
import { useProposalDone } from '@/api/proposals'
import { errorText } from '@/client/gqlClient'
import { answerEmpty, type Answer } from '@/lib/dossier'
import { stateWords } from '@/lib/proposal'
import { canMarkDone } from '@/lib/review'
import { kindWords, stackTone, stackWords } from '@/lib/stack'
import { ActionCard } from '@/components/stack/ActionCard'
import { ChangeCard } from '@/components/stack/ChangeCard'
import { DocEditCard } from '@/components/stack/DocEditCard'
import { GenericCard } from '@/components/stack/GenericCard'
import { ItemStatus } from '@/components/stack/ItemStatus'
import { MessageCard } from '@/components/stack/MessageCard'
import { QuestionCard } from '@/components/stack/QuestionCard'
import { UpstreamCard } from '@/components/stack/UpstreamCard'
import {
  ActionBar,
  BodyFileBox,
  DoneForm,
  EvidenceBox,
  NoteField,
  noteIdOf,
  ReviewStateChip,
  Row,
  Thread,
  Weighing,
  type CardProps,
  type Item,
} from '@/components/stack/parts'

// editStart is what "Edit, then approve" starts from: the agent's own version. A body that names
// a vault file starts from the file's text, which replaces the path.
function editStart(it: Item): Answer | undefined {
  switch (it.kind) {
    case 'para':
    case 'op':
      return { input: it.input }
    case 'message':
      return { subject: it.subject ?? '', body: it.body ?? '', people: [...(it.people ?? [])] }
    case 'external':
    case 'action':
      return { subject: it.subject ?? '', body: it.bodyFile?.text ?? it.body ?? '' }
    case 'doc_edit':
      return { new: it.new ?? '' }
    default:
      return undefined
  }
}

function Body(p: CardProps) {
  switch (p.it.kind) {
    case 'question':
      return <QuestionCard {...p} />
    case 'para':
    case 'op':
      return <ChangeCard {...p} />
    case 'message':
    case 'external':
      return <MessageCard {...p} />
    case 'action':
      return <ActionCard {...p} />
    case 'doc_edit':
      return <DocEditCard {...p} />
    case 'upstream':
      return <UpstreamCard {...p} />
    default:
      return <GenericCard {...p} />
  }
}

// doneStart is the text Done by me starts from: what was sent, as the owner edited it. A doc edit
// starts empty: its text is the whole document after the edit.
function doneStart(it: Item, answer: Answer | null): string {
  if (it.kind === 'doc_edit') return ''
  if (answer?.decision === 'edit' && answer.body) return answer.body
  return it.bodyFile?.text ?? it.body ?? ''
}

// ItemCard is one item of a dossier, in place in the narrative. It shows the item, the owner's
// answer, and what became of it. onAnswer is set only while the owner may answer. proposal is
// set when the owner may mark an outward item done by themself (Done by me), which takes effect at
// once. current marks the card the page keys act on; onCurrent says the owner clicked in it.
export function ItemCard({
  it,
  answer,
  onAnswer,
  stale,
  bundleSize,
  proposal,
  current,
  onCurrent,
}: CardProps & {
  stale?: boolean
  bundleSize?: number
  proposal?: string
  current?: boolean
  onCurrent?: () => void
}) {
  const start = editStart(it)
  const open = it.stack === 'proposed'
  const drafted = open && !answerEmpty(answer)
  const noteId = noteIdOf(it.id)
  const [doing, setDoing] = useState(false)
  const done = useProposalDone()
  const mayDo = !!proposal && canMarkDone(it)
  const doneButton = mayDo ? (
    <Button
      size="small"
      data-act="done"
      variant={doing ? 'contained' : 'outlined'}
      aria-pressed={doing}
      onClick={() => setDoing((x) => !x)}
    >
      Done by me
    </Button>
  ) : null
  return (
    <Paper
      variant="outlined"
      sx={{
        p: 1.5,
        my: 1.5,
        borderColor: stale ? 'warning.main' : current ? 'primary.main' : undefined,
      }}
      id={`item-${it.id}`}
      data-item={it.id}
      data-stack={it.stack}
      data-state={it.state}
      aria-current={current || undefined}
      onClickCapture={onCurrent}
      onFocusCapture={onCurrent}
    >
      <Typography variant="subtitle2" component="h3" sx={{ mb: 0.5, overflowWrap: 'anywhere' }}>
        {it.title || it.summary}
      </Typography>
      <Stack direction="row" spacing={1} useFlexGap sx={{ mb: 1, flexWrap: 'wrap' }}>
        <StatusChip tone="quiet" label={kindWords(it.kind)} />
        <StatusChip tone={stackTone(it.stack)} label={stackWords(it.stack)} />
        {it.state === 'skipped' || it.state === 'sent_back' ? (
          <ReviewStateChip state={it.state} />
        ) : (
          it.state !== 'pending' && (
            <StateChip state={it.state} label={stateWords(it.state, it.kind)} />
          )
        )}
        {drafted && <StatusChip tone="wait" label="drafted" />}
        {answer?.via === 'file' && <StatusChip tone="attn" label="typed in the file" />}
        {stale && <StatusChip tone="attn" label="answer is stale" />}
        {it.edited && <StatusChip tone="wait" label="edited" />}
        {it.heldBy && <StatusChip tone="attn" label={`needs ${it.heldBy} first`} />}
        {it.bundle && (
          <StatusChip
            tone="quiet"
            label={`bundle ${it.bundle}${bundleSize ? ` (${bundleSize}, decided as one)` : ''}`}
          />
        )}
      </Stack>
      {stale && (
        <Alert
          severity="warning"
          sx={{ mb: 1 }}
          action={
            onAnswer && answer ? (
              <Button color="inherit" size="small" onClick={() => onAnswer({ ...answer })}>
                Keep my answer
              </Button>
            ) : undefined
          }
        >
          The agent changed this item after you answered. Check your answer again.
        </Alert>
      )}
      {it.bodyFile && <BodyFileBox file={it.bodyFile} />}
      <Body it={it} answer={answer} onAnswer={onAnswer} />
      <Weighing it={it} />
      <EvidenceBox it={it} />
      {it.state === 'sent_back' && (answer?.comment || (answer?.refs ?? []).length > 0) && (
        <Alert severity="info" sx={{ my: 1 }} data-sent-back>
          You asked the agent: {answer?.comment}
          {(answer?.refs ?? []).length > 0 && <> Read: {(answer?.refs ?? []).join(', ')}.</>}
        </Alert>
      )}
      {onAnswer ? (
        <ActionBar
          answer={answer}
          onAnswer={onAnswer}
          question={it.kind === 'question'}
          start={start}
          noteId={noteId}
          extra={doneButton}
        />
      ) : (
        doneButton && (
          <Stack direction="row" sx={{ my: 1 }}>
            {doneButton}
          </Stack>
        )
      )}
      {doing && proposal && (
        <DoneForm
          start={doneStart(it, answer)}
          textLabel={
            it.kind === 'doc_edit'
              ? 'The document after your edit (optional)'
              : 'What you sent (optional)'
          }
          withNote
          busy={done.isPending}
          error={done.isError ? errorText(done.error) : undefined}
          onCancel={() => setDoing(false)}
          onConfirm={(f) => {
            const orig = it.kind === 'doc_edit' ? '' : (it.bodyFile?.text ?? it.body ?? '')
            const text = f.text.trim() && f.text !== orig ? f.text : undefined
            done.mutate(
              {
                id: proposal,
                item: it.id,
                url: f.url.trim() || undefined,
                locator: f.locator.trim() || undefined,
                text,
                note: f.note.trim() || undefined,
              },
              { onSuccess: () => setDoing(false) },
            )
          }}
        />
      )}
      {!onAnswer && answer && open && (
        <Row
          label="Your draft"
          text={[answer.decision, (answer.choices ?? []).join(', '), answer.ownWords]
            .filter(Boolean)
            .join(' - ')}
        />
      )}
      {onAnswer ? (
        <NoteField answer={answer} onAnswer={onAnswer} id={noteId} />
      ) : (
        <Row label="Your note" text={answer?.comment} />
      )}
      <Thread it={it} />
      {!open && <ItemStatus it={it} />}
      <Box>
        <Details summary="Hashes">
          {it.approvedHash && (
            <Typography variant="caption" component="div">
              Approved as hash <code>{it.approvedHash}</code>
            </Typography>
          )}
          <Typography variant="caption" component="div">
            An approval covers this exact text. Hash <code>{it.hash}</code>
          </Typography>
        </Details>
      </Box>
    </Paper>
  )
}
