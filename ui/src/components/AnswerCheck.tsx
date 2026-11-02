import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Link from '@mui/material/Link'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useDoc } from '@/api/doc'
import { useAcceptAnswer, type useQuestions } from '@/api/questions'
import { errorText } from '@/client/gqlClient'
import { DocLink } from '@/components/DocLink'
import { LinkButton } from '@/components/links'
import { isUrl, proposedText } from '@/lib/answer'
import { humanTime } from '@/lib/when'
import { joinNames, nameOr } from '@/lib/names'

type Question = NonNullable<ReturnType<typeof useQuestions>['data']>[number]
type Source = { ref: string; quote?: string | null }

// SourceDoc links a vault document by its title, or its name made from the id while it loads.
function SourceDoc({ id }: { id: string }) {
  const d = useDoc(id)
  return <DocLink id={id}>{d.data?.title || nameOr('', id)}</DocLink>
}

// SourceList shows where an agent found its answer. A web address opens in a new tab; anything
// else is a vault document.
export function SourceList({ sources }: { sources: readonly Source[] | null | undefined }) {
  const list = sources ?? []
  if (list.length === 0) return null
  return (
    <Box component="ul" aria-label="Sources" sx={{ m: 0, pl: 2.5 }}>
      {list.map((s) => (
        <li key={s.ref}>
          <Typography variant="body2" component="span" sx={{ overflowWrap: 'anywhere' }}>
            {isUrl(s.ref) ? (
              <Link href={s.ref} target="_blank" rel="noopener noreferrer">
                {s.ref}
              </Link>
            ) : (
              <SourceDoc id={s.ref} />
            )}
          </Typography>
          {s.quote && (
            <Typography
              variant="body2"
              color="text.secondary"
              sx={{ fontStyle: 'italic', overflowWrap: 'anywhere' }}
            >
              &ldquo;{s.quote}&rdquo;
            </Typography>
          )}
        </li>
      ))}
    </Box>
  )
}

// AnswerCard is one agent answer to check: the question, what the agent proposes and why, the
// sources, and Accept (the text can be changed first) or Reject (a reason is required).
export function AnswerCard({ q, onAccepted }: { q: Question; onAccepted?: (m: string) => void }) {
  const accept = useAcceptAnswer()
  const initial = proposedText(q)
  const [text, setText] = useState(initial)
  const changed = text.trim() !== initial
  const submit = () =>
    accept.mutate(
      {
        id: q.id,
        // Unchanged text is accepted as proposed. A changed text replaces it; the sources stay.
        text: changed ? text.trim() : undefined,
      },
      {
        onSuccess: (r) =>
          onAccepted?.(
            r.unblocked.length
              ? `Accepted "${q.title}". Unblocked ${joinNames(r.unblocked.map((u) => nameOr('', u)))}.`
              : `Accepted "${q.title}".`,
          ),
      },
    )
  return (
    <Stack spacing={1} sx={{ py: 1 }}>
      <Typography sx={{ fontWeight: 600, overflowWrap: 'anywhere' }}>
        <DocLink id={q.id}>{q.title || q.id}</DocLink>
      </Typography>
      <Typography variant="caption" color="text.secondary">
        Proposed by {q.proposedBy || 'an agent'}
        {q.proposed ? `, ${humanTime(q.proposed)}` : ''}
      </Typography>
      {q.proposedWhy && (
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          Reason: {q.proposedWhy}
        </Typography>
      )}
      <SourceList sources={q.proposedSources} />
      {accept.isError && <Alert severity="error">{errorText(accept.error)}</Alert>}
      <TextField
        label="Answer"
        value={text}
        onChange={(e) => setText(e.target.value)}
        multiline
        minRows={2}
        fullWidth
        size="small"
        helperText={changed ? 'Changed. The sources stay with it.' : 'Edit before you accept.'}
      />
      <Stack direction="row" spacing={1}>
        <Button
          size="small"
          variant="contained"
          disabled={accept.isPending || text.trim() === ''}
          onClick={submit}
          aria-label={`Accept answer: ${q.title}`}
        >
          Accept
        </Button>
        <LinkButton
          size="small"
          color="error"
          to="."
          search={(p) => ({ ...p, reject: q.id })}
          aria-label={`Reject answer: ${q.title}`}
        >
          Reject
        </LinkButton>
      </Stack>
    </Stack>
  )
}
