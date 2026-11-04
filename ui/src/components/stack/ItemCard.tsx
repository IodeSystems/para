import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { Details, StateChip, StatusChip } from '@/components/Bits'
import { answerEmpty, type Answer } from '@/lib/dossier'
import { stateWords } from '@/lib/proposal'
import { kindWords, stackTone, stackWords } from '@/lib/stack'
import { ActionCard } from '@/components/stack/ActionCard'
import { ChangeCard } from '@/components/stack/ChangeCard'
import { DocEditCard } from '@/components/stack/DocEditCard'
import { GenericCard } from '@/components/stack/GenericCard'
import { ItemStatus } from '@/components/stack/ItemStatus'
import { MessageCard } from '@/components/stack/MessageCard'
import { QuestionCard } from '@/components/stack/QuestionCard'
import {
  CommentField,
  DecisionBar,
  EvidenceBox,
  Row,
  Thread,
  Weighing,
  type CardProps,
  type Item,
} from '@/components/stack/parts'

// editStart is what "Edit, then approve" starts from: the agent's own version.
function editStart(it: Item): Answer | undefined {
  switch (it.kind) {
    case 'para':
    case 'op':
      return { input: it.input }
    case 'message':
    case 'external':
    case 'action':
      return { subject: it.subject ?? '', body: it.body ?? '' }
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
    default:
      return <GenericCard {...p} />
  }
}

// ItemCard is one item of a dossier, in place in the narrative. It shows the item, the owner's
// answer, and what became of it. onAnswer is set only while the owner may answer.
export function ItemCard({
  it,
  answer,
  onAnswer,
  stale,
  bundleSize,
}: CardProps & { stale?: boolean; bundleSize?: number }) {
  const start = editStart(it)
  const open = it.stack === 'proposed'
  const drafted = open && !answerEmpty(answer)
  return (
    <Paper
      variant="outlined"
      sx={{ p: 1.5, my: 1.5, borderColor: stale ? 'warning.main' : undefined }}
      id={`item-${it.id}`}
      data-item={it.id}
      data-stack={it.stack}
    >
      <Typography variant="subtitle2" component="h3" sx={{ mb: 0.5, overflowWrap: 'anywhere' }}>
        {it.title || it.summary}
      </Typography>
      <Stack direction="row" spacing={1} useFlexGap sx={{ mb: 1, flexWrap: 'wrap' }}>
        <StatusChip tone="quiet" label={kindWords(it.kind)} />
        <StatusChip tone={stackTone(it.stack)} label={stackWords(it.stack)} />
        {it.state !== 'pending' && (
          <StateChip state={it.state} label={stateWords(it.state, it.kind)} />
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
      <Body it={it} answer={answer} onAnswer={onAnswer} />
      <Weighing it={it} />
      <EvidenceBox it={it} />
      {onAnswer && it.kind !== 'question' && (
        <DecisionBar
          answer={answer}
          onAnswer={onAnswer}
          canEdit={start !== undefined}
          onEdit={() => start ?? {}}
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
        <CommentField answer={answer} onAnswer={onAnswer} />
      ) : (
        <Row label="Your comment" text={answer?.comment} />
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
