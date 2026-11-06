import Box from '@mui/material/Box'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { StateChip } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'
import { Markdown } from '@/components/Markdown'
import { nameOr } from '@/lib/names'

// An annotation is a note, exception, convention, or constraint written inline in an area body
// as an Obsidian callout. It shows as a card where it sits in the text: its kind, its fields, and
// its reason. Lost: its rule is gone from the body, or its quote from the newest source version.

const KIND_WORDS: Record<string, string> = {
  note: 'Note',
  exception: 'Exception',
  convention: 'Convention',
  constraint: 'Constraint',
}

const KIND_COLOR: Record<string, string> = {
  note: 'info.main',
  exception: 'warning.main',
  convention: 'success.main',
  constraint: 'error.main',
}

// FIELD_WORDS names the header fields in words; anchor, quote, version, and from show apart.
const FIELD_WORDS: Record<string, string> = {
  kind: '',
  by: 'by',
  with: 'with',
  why: 'why',
  source: 'source',
  applies_in: 'applies in',
  not_in: 'not in',
  refines: 'refines',
  policy_text: 'policy',
}

const shown = new Set(['anchor', 'quote', 'version', 'from', 'status', 'review_by'])

// Annotation is the AnnotationFields fragment (api/areas.ts).
export type Annotation = {
  id: string
  kind: string
  anchor?: string | null
  text: string
  reviewBy?: string | null
  quote?: string | null
  lost?: boolean | null
  from?: string | null
  fields: { key: string; value: string }[]
}

export function AnnotationCard({ annotation: a }: { annotation: Annotation }) {
  const field = (k: string) => a.fields.find((f) => f.key === k)?.value ?? ''
  const status = field('status')
  const details = a.fields
    .filter((f) => !shown.has(f.key))
    .map((f) => {
      const w = FIELD_WORDS[f.key] ?? f.key.replace(/_/g, ' ')
      const v = f.value.replace(/\[\[([^\]|#]+)[^\]]*\]\]/g, (_m, id: string) => nameOr('', id))
      return w ? `${w} ${v}` : v.replace(/_/g, ' ')
    })
  return (
    <Box
      component="aside"
      data-annotation={a.id}
      data-kind={a.kind}
      sx={{
        borderLeft: 4,
        borderColor: KIND_COLOR[a.kind] ?? 'divider',
        bgcolor: 'action.hover',
        borderRadius: 1,
        px: 1.5,
        py: 1,
        my: 1,
      }}
    >
      <Stack
        direction="row"
        spacing={1}
        useFlexGap
        sx={{ alignItems: 'center', flexWrap: 'wrap', mb: 0.5 }}
      >
        <Typography variant="subtitle2" component="span">
          {KIND_WORDS[a.kind] ?? a.kind}
        </Typography>
        {status && <StateChip state={status} />}
        {a.anchor && (
          <Typography variant="caption" color="text.secondary">
            on {a.anchor.replace(/^\^/, '')}
          </Typography>
        )}
        {a.reviewBy && (
          <Typography variant="caption" color="text.secondary">
            review by {a.reviewBy}
          </Typography>
        )}
        {a.lost && (
          <Typography variant="caption" color="warning.main">
            {a.quote ? 'its quote is not in the newest source' : 'its rule is gone'}
          </Typography>
        )}
      </Stack>
      {details.length > 0 && (
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ display: 'block', mb: 0.5, overflowWrap: 'anywhere' }}
        >
          {details.join(' - ')}
        </Typography>
      )}
      {a.quote && (
        <Typography variant="body2" sx={{ fontStyle: 'italic', mb: 0.5, overflowWrap: 'anywhere' }}>
          “{a.quote}”
        </Typography>
      )}
      <Markdown source={a.text} />
      {a.from && (
        <Typography variant="caption" color="text.secondary">
          Folded from <DocLink id={a.from}>{nameOr('', a.from)}</DocLink>
        </Typography>
      )}
    </Box>
  )
}
