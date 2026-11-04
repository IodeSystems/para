import Box from '@mui/material/Box'
import Link from '@mui/material/Link'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useDoc } from '@/api/doc'
import type { useQuestions } from '@/api/questions'
import { DocLink } from '@/components/DocLink'
import { isUrl, proposedText } from '@/lib/answer'
import { humanTime } from '@/lib/when'
import { nameOr } from '@/lib/names'

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

// ProposedAnswer shows an answer an older agent version proposed. Read only: agent answers are
// final now, and the owner answers in the chat.
export function ProposedAnswer({ q }: { q: Question }) {
  return (
    <Stack spacing={1} sx={{ py: 1 }}>
      <Typography variant="subtitle2">Proposed answer (older agent version)</Typography>
      <Typography variant="caption" color="text.secondary">
        Proposed by {q.proposedBy || 'an agent'}
        {q.proposed ? `, ${humanTime(q.proposed)}` : ''}
      </Typography>
      <Typography sx={{ overflowWrap: 'anywhere' }}>{proposedText(q)}</Typography>
      {q.proposedWhy && (
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          Reason: {q.proposedWhy}
        </Typography>
      )}
      <SourceList sources={q.proposedSources} />
    </Stack>
  )
}
