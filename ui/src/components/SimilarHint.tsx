import Typography from '@mui/material/Typography'
import { useSimilar } from '@/api/similar'
import type { Para_SimilarType } from '@/gql/graphql'

// SimilarHint is the quiet note for a person's create. It never stops one.
export function SimilarHint({ type, title }: { type: Para_SimilarType; title: string }) {
  const first = useSimilar(type, title).data?.[0]
  if (!first) return null
  return (
    <Typography variant="caption" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
      similar: {first.title}
    </Typography>
  )
}
